package config

import (
	"fmt"
	"os"
	"pulsar/internal/logger"
	"strconv"
	"sync"
)

// Contains environment variables
type Environ struct {
	EnvName             string
	Hostname            string
	Port                int
	AllowedOrigins      string
	MaxRequestSizeBytes int64
	RateLimitRps        float64
	RateLimitBurst      int
	InitWaitSecs        int
	DisconnectGraceSecs int
	RoomInactivitySecs  int
	WsWriteWaitSecs     int
	WsPongWaitSecs      int
	WsMaxMessageSizeBytes int64
}

type EnvName string

const (
	DEVELOPMENT EnvName = "development"
	PRODUCTION  EnvName = "production"
)

// Provides the address in format "hostname:port"
func (e Environ) Address() string {
	return fmt.Sprintf("%s:%d", e.Hostname, e.Port)
}

// Read string value from environment
func readString(key string, defaultValue string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

// Read integer value from environment
func readInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	intValue, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return defaultValue
	}
	return int(intValue)
}

// Read int64 value from environment
func readInt64(key string, defaultValue int64) int64 {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	intValue, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return defaultValue
	}
	return intValue
}

// Read float64 value from environment
func readFloat64(key string, defaultValue float64) float64 {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return defaultValue
	}
	return floatValue
}

// Read environment variables from environment
func loadEnv() Environ {
	hostnameKey := readString("HOSTNAMEKEY", "HOSTNAME")
	environ := Environ{
		EnvName:               readString("ENV", string(PRODUCTION)),
		Hostname:              readString(hostnameKey, ""),
		Port:                  readInt("PORT", 8000),
		AllowedOrigins:        readString("ALLOWED_ORIGINS", ""),
		MaxRequestSizeBytes:   readInt64("MAX_REQUEST_SIZE_BYTES", 1048576), // 1MB
		RateLimitRps:          readFloat64("RATE_LIMIT_RPS", 20.0),
		RateLimitBurst:        readInt("RATE_LIMIT_BURST", 30),
		InitWaitSecs:          readInt("INIT_WAIT_SECS", 120),
		DisconnectGraceSecs:   readInt("DISCONNECT_GRACE_SECS", 30),
		RoomInactivitySecs:    readInt("ROOM_INACTIVITY_SECS", 1800),
		WsWriteWaitSecs:       readInt("WS_WRITE_WAIT_SECS", 10),
		WsPongWaitSecs:        readInt("WS_PONG_WAIT_SECS", 60),
		WsMaxMessageSizeBytes: readInt64("WS_MAX_MESSAGE_SIZE_BYTES", 512*1024), // 512KB
	}

	if len(environ.Hostname) == 0 && environ.EnvName == string(PRODUCTION) {
		logger.Fatalf("Failed to read hostname with key '%q' in %s", hostnameKey, environ.EnvName)
	}

	return environ
}

var (
	env  Environ
	once sync.Once
)

// Loads environment variables.
func LoadEnv() Environ {
	once.Do(func() {
		env = loadEnv()
		logger.Infof("Loaded %s environment", env.EnvName)
	})
	return env
}

// ResetEnv resets the cached configuration (useful for testing different configs).
func ResetEnv() {
	once = sync.Once{}
	env = loadEnv()
}

// Know if the environemt is development
func IsDevEnv() bool {
	return env.EnvName == string(DEVELOPMENT)
}

// Know if the environemt is production
func IsProdEnv() bool {
	return env.EnvName == string(PRODUCTION)
}
