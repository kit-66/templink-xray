package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/vless"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Access struct {
	UUID      string
	ExpiresAt time.Time
}

type Manager struct {
	mu     sync.Mutex
	access *Access
	client command.HandlerServiceClient
}

var (
	manager Manager

	ttl         time.Duration
	xrayInbound string
	vlessHost   string
	vlessPort   string
	vlessPath   string
	vlessName   string
	logoPath    string
)

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func makeVLESS(id string) string {
	q := url.Values{}
	q.Set("type", "ws")
	q.Set("security", "tls")
	q.Set("path", vlessPath)
	q.Set("host", vlessHost)
	q.Set("sni", vlessHost)

	return fmt.Sprintf(
		"vless://%s@%s:%s?%s#%s",
		id,
		vlessHost,
		vlessPort,
		q.Encode(),
		url.QueryEscape(vlessName),
	)
}

func addUser(id string) error {
	account := serial.ToTypedMessage(&vless.Account{
		Id: id,
	})

	user := &protocol.User{
		Email:   "temporary",
		Level:   0,
		Account: account,
	}

	operation := serial.ToTypedMessage(&command.AddUserOperation{
		User: user,
	})

	_, err := manager.client.AlterInbound(
		context.Background(),
		&command.AlterInboundRequest{
			Tag:       xrayInbound,
			Operation: operation,
		},
	)

	return err
}

func removeUser() error {
	operation := serial.ToTypedMessage(&command.RemoveUserOperation{
		Email: "temporary",
	})

	_, err := manager.client.AlterInbound(
		context.Background(),
		&command.AlterInboundRequest{
			Tag:       xrayInbound,
			Operation: operation,
		},
	)

	return err
}

func createAccess() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.access != nil {
		if err := removeUser(); err != nil {
			log.Printf("remove old user: %v", err)
		}
	}

	id := uuid.NewString()
	expires := time.Now().Add(ttl)

	if err := addUser(id); err != nil {
		return err
	}

	manager.access = &Access{
		UUID:      id,
		ExpiresAt: expires,
	}

	log.Printf(
		"new temporary access created, expires at %s",
		expires.Format(time.RFC3339),
	)

	return nil
}

func ensureAccess() error {
	manager.mu.Lock()

	if manager.access != nil &&
		time.Now().Before(manager.access.ExpiresAt) {
		manager.mu.Unlock()
		return nil
	}

	manager.mu.Unlock()

	return createAccess()
}

func accessHandler(w http.ResponseWriter, r *http.Request) {
	if err := ensureAccess(); err != nil {
		log.Printf("access error: %v", err)
		http.Error(w, "Xray API error", http.StatusInternalServerError)
		return
	}

	manager.mu.Lock()
	access := *manager.access
	manager.mu.Unlock()

	seconds := int(time.Until(access.ExpiresAt).Seconds())
	if seconds < 0 {
		seconds = 0
	}

	response := map[string]interface{}{
		"uuid":       access.UUID,
		"vless":      makeVLESS(access.UUID),
		"expires_at": access.ExpiresAt.Unix(),
		"expires_in": seconds,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("json response error: %v", err)
	}
}

// overlayLogoOnQR добавляет логотип в центр QR-кода
func overlayLogoOnQR(qrImage []byte, logPath string) ([]byte, error) {
	// Декодируем QR-код
	qrImg, err := png.Decode(bytes.NewReader(qrImage))
	if err != nil {
		return nil, fmt.Errorf("decode QR: %w", err)
	}

	// Открываем логотип
	logoFile, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("open logo: %w", err)
	}
	defer logoFile.Close()

	logoImg, err := png.Decode(logoFile)
	if err != nil {
		return nil, fmt.Errorf("decode logo: %w", err)
	}

	// Получаем размеры
	qrBounds := qrImg.Bounds()
	logoBounds := logoImg.Bounds()

	qrWidth := qrBounds.Dx()
	qrHeight := qrBounds.Dy()
	logoWidth := logoBounds.Dx()
	logoHeight := logoBounds.Dy()

	// Размер логотипа: 25% от размера QR-кода
	maxLogoSize := qrWidth / 4
	var resizedLogoWidth, resizedLogoHeight int

	if logoWidth > maxLogoSize {
		ratio := float64(logoHeight) / float64(logoWidth)
		resizedLogoWidth = maxLogoSize
		resizedLogoHeight = int(float64(maxLogoSize) * ratio)
	} else {
		resizedLogoWidth = logoWidth
		resizedLogoHeight = logoHeight
	}

	// Создаём новое изображение RGBA для результата
	result := image.NewRGBA(qrBounds)

	// Копируем QR-код
	draw.Draw(result, qrBounds, qrImg, qrBounds.Min, draw.Src)

	// Вычисляем позицию логотипа (центр QR-кода)
	startX := (qrWidth - resizedLogoWidth) / 2
	startY := (qrHeight - resizedLogoHeight) / 2

	// Масштабируем и накладываем логотип
	for y := 0; y < resizedLogoHeight; y++ {
		for x := 0; x < resizedLogoWidth; x++ {
			// Вычисляем исходные координаты в логотипе
			srcX := logoBounds.Min.X + (x*logoWidth)/resizedLogoWidth
			srcY := logoBounds.Min.Y + (y*logoHeight)/resizedLogoHeight

			// Получаем пиксель из логотипа
			r, g, b, a := logoImg.At(srcX, srcY).RGBA()

			// Если пиксель не полностью прозрачен, добавляем его на результат
			if a > 0 {
				result.SetRGBA(startX+x, startY+y, color.RGBA{
					R: uint8(r >> 8),
					G: uint8(g >> 8),
					B: uint8(b >> 8),
					A: uint8(a >> 8),
				})
			}
		}
	}

	// Кодируем результат в PNG
	buf := &bytes.Buffer{}
	if err := png.Encode(buf, result); err != nil {
		return nil, fmt.Errorf("encode PNG: %w", err)
	}

	return buf.Bytes(), nil
}

func qrHandler(w http.ResponseWriter, r *http.Request) {
	if err := ensureAccess(); err != nil {
		log.Printf("qr access error: %v", err)
		http.Error(w, "Xray API error", http.StatusInternalServerError)
		return
	}

	manager.mu.Lock()
	link := makeVLESS(manager.access.UUID)
	manager.mu.Unlock()

	qrPNG, err := qrcode.Encode(link, qrcode.Medium, 320)
	if err != nil {
		log.Printf("qr error: %v", err)
		http.Error(w, "QR error", http.StatusInternalServerError)
		return
	}

	// Добавляем логотип, если он существует
	if logoPath != "" {
		if _, err := os.Stat(logoPath); err == nil {
			qrWithLogo, err := overlayLogoOnQR(qrPNG, logoPath)
			if err != nil {
				log.Printf("logo overlay error: %v", err)
				// Продолжаем с исходным QR-кодом без логотипа
			} else {
				qrPNG = qrWithLogo
			}
		}
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")

	_, _ = w.Write(qrPNG)
}

func main() {
	minutes, err := strconv.Atoi(env("ACCESS_TTL_MINUTES", "10"))
	if err != nil || minutes <= 0 {
		log.Fatal("invalid ACCESS_TTL_MINUTES")
	}

	ttl = time.Duration(minutes) * time.Minute

	xrayInbound = env("XRAY_INBOUND_TAG", "vless-ws")

	vlessHost = env(
		"VLESS_HOST",
		"templink.mycloud.ip-ddns.com",
	)

	vlessPort = env("VLESS_PORT", "443")
	vlessPath = env("VLESS_PATH", "/xray")
	vlessName = env("VLESS_NAME", "TempLink")

	xrayAPIAddr := env("XRAY_API_ADDR", "127.0.0.1")
	xrayAPIPort := env("XRAY_API_PORT", "10085")

	webAddr := env("WEB_ADDR", "127.0.0.1")
	webPort := env("WEB_PORT", "3002")

	// Логотип загружается из /app/assets/logo.png по умолчанию
	logoPath = env("LOGO_PATH", "/app/assets/logo.png")

	xrayAPI := xrayAPIAddr + ":" + xrayAPIPort
	listenAddr := webAddr + ":" + webPort

	conn, err := grpc.NewClient(
		xrayAPI,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("connect to Xray API: %v", err)
	}
	defer conn.Close()

	manager.client = command.NewHandlerServiceClient(conn)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/access", accessHandler)
	mux.HandleFunc("/api/qr", qrHandler)

	mux.Handle(
		"/",
		http.FileServer(http.Dir("/app/site")),
	)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("web listening on %s", listenAddr)
	log.Printf("xray api: %s", xrayAPI)
	log.Printf("xray inbound: %s", xrayInbound)
	log.Printf("access TTL: %d minutes", minutes)
	if _, err := os.Stat(logoPath); err == nil {
		log.Printf("logo loaded from: %s", logoPath)
	} else {
		log.Printf("logo not found at: %s", logoPath)
	}

	if err := server.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
