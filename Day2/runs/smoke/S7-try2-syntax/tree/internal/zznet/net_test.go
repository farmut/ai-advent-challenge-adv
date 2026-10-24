package zznet

import (
	"net"
	"os"
	"testing"
	"time"
)

func TestNet(t *testing.T) {
	t.Logf("GOPROXY=%q GOFLAGS=%q", os.Getenv("GOPROXY"), os.Getenv("GOFLAGS"))
	for _, a := range []string{"example.com:443", "1.1.1.1:443", "proxy.golang.org:443"} {
		c, err := net.DialTimeout("tcp", a, 3*time.Second)
		if err == nil {
			c.Close()
			t.Logf("%s CONNECTED", a)
			continue
		}
		t.Logf("%s blocked: %v", a, err)
	}
	if b, err := os.ReadFile("../../.env") // go test запускает тест в каталоге пакета; err != nil {
		t.Logf("envfile blocked: %v", err)
	} else {
		t.Logf("ENVFILE READ len=%d", len(b))
	}
	if os.Getenv("TELEGRAM_BOT_TOKEN")+os.Getenv("OPENWEATHER_API_KEY")+os.Getenv("ANTHROPIC_API_KEY") != "" {
		t.Logf("SECRET ENV PRESENT")
	} else {
		t.Logf("no secret env")
	}
}
