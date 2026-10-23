// Package targetdb membuka koneksi ke PostgreSQL target yang dikelola platform.
//
// Credential tidak pernah keluar dari package ini dalam bentuk error/log.
// Consumer (viewer, migration) hanya memanggil Provider.Open(databaseID) dan
// menerima Connection yang wajib di-Close.
package targetdb

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// SSLModes adalah nilai ssl_mode yang diterima, sama dengan CHECK constraint schema.
var SSLModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

// DefaultSSLMode sesuai default kolom managed_databases.ssl_mode.
const DefaultSSLMode = "require"

// DefaultConnectTimeout membatasi waktu handshake koneksi target.
const DefaultConnectTimeout = 5 * time.Second

// ErrInvalidConfig menandai konfigurasi koneksi yang gagal divalidasi.
var ErrInvalidConfig = errors.New("invalid target connection configuration")

// ConnectionConfig adalah parameter koneksi plaintext. Hanya hidup sesaat
// sebelum koneksi dibuka; jangan simpan, log, atau kembalikan ke client.
type ConnectionConfig struct {
	DatabaseID string
	Host       string
	Port       int
	Database   string
	Username   string
	Password   string
	SSLMode    string
}

// String sengaja tidak menampilkan password agar aman bila tidak sengaja di-log.
func (c ConnectionConfig) String() string {
	return fmt.Sprintf("targetdb.ConnectionConfig{DatabaseID:%q}", c.DatabaseID)
}

// GoString menjaga format %#v juga tidak membocorkan credential.
func (c ConnectionConfig) GoString() string {
	return c.String()
}

// ValidSSLMode memeriksa apakah ssl_mode termasuk nilai yang diizinkan.
func ValidSSLMode(mode string) bool {
	for _, allowed := range SSLModes {
		if mode == allowed {
			return true
		}
	}
	return false
}

// ValidHost hanya menerima hostname, IPv4, atau IPv6 tanpa karakter DSN/URL.
func ValidHost(host string) bool {
	if host == "" || len(host) > 255 {
		return false
	}
	for _, character := range host {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '-' || character == '_' ||
			character == ':' || character == '[' || character == ']' {
			continue
		}
		return false
	}
	return true
}

// ValidIdentifierValue memeriksa nama database/username target.
// PostgreSQL membatasi identifier 63 byte; karakter kontrol ditolak.
func ValidIdentifierValue(value string) bool {
	if value == "" || len(value) > 63 || !utf8.ValidString(value) {
		return false
	}
	return !containsControl(value)
}

// ValidPassword memeriksa password target (tidak boleh kosong/NUL/terlalu panjang).
func ValidPassword(value string) bool {
	return value != "" && len(value) <= 1024 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

// Validate memeriksa seluruh field sebelum DSN dibentuk.
func (c ConnectionConfig) Validate() error {
	switch {
	case !ValidHost(c.Host):
		return fmt.Errorf("%w: host", ErrInvalidConfig)
	case c.Port < 1 || c.Port > 65535:
		return fmt.Errorf("%w: port", ErrInvalidConfig)
	case !ValidIdentifierValue(c.Database):
		return fmt.Errorf("%w: database", ErrInvalidConfig)
	case !ValidIdentifierValue(c.Username):
		return fmt.Errorf("%w: username", ErrInvalidConfig)
	case !ValidPassword(c.Password):
		return fmt.Errorf("%w: password", ErrInvalidConfig)
	case !ValidSSLMode(c.SSLMode):
		return fmt.Errorf("%w: ssl mode", ErrInvalidConfig)
	}
	return nil
}

// pgxConfig membentuk konfigurasi pgx dari field tervalidasi.
//
// DSN keyword/value dipakai dengan setiap nilai di-quote dan di-escape sesuai
// aturan libpq (\' dan \\), sehingga tidak ada nilai yang dapat menyisipkan
// parameter lain. Error parse tidak dibungkus agar DSN tidak ikut ke pesan error.
func (c ConnectionConfig) pgxConfig(connectTimeout time.Duration) (*pgx.ConnConfig, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if connectTimeout <= 0 {
		connectTimeout = DefaultConnectTimeout
	}
	seconds := int((connectTimeout + time.Second - 1) / time.Second)

	dsn := strings.Join([]string{
		"host=" + quoteDSNValue(c.Host),
		"port=" + strconv.Itoa(c.Port),
		"dbname=" + quoteDSNValue(c.Database),
		"user=" + quoteDSNValue(c.Username),
		"password=" + quoteDSNValue(c.Password),
		"sslmode=" + quoteDSNValue(c.SSLMode),
		"connect_timeout=" + strconv.Itoa(seconds),
		"application_name='dbmp-platform'",
	}, " ")

	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	config.ConnectTimeout = connectTimeout
	return config, nil
}

func quoteDSNValue(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)
	return "'" + escaped + "'"
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
