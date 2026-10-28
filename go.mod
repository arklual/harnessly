module github.com/ly-solutions/harnessly

go 1.26.0

// The front-end's npm packages may carry Go files of their own.
ignore ./web/node_modules

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/coder/websocket v1.8.15
	github.com/creack/pty v1.1.24
	github.com/dlclark/regexp2 v1.12.0
	github.com/fsnotify/fsnotify v1.10.1
	github.com/segmentio/asm v1.2.1
	github.com/yuin/goldmark v1.8.6
	golang.org/x/text v0.43.0
)

require golang.org/x/sys v0.49.0

require (
	github.com/andybalholm/brotli v1.2.6
	github.com/coreos/go-oidc/v3 v3.21.0
	github.com/go-webauthn/webauthn v0.18.2
	github.com/jackc/pgx/v5 v5.11.0
	golang.org/x/oauth2 v0.37.0
	modernc.org/sqlite v1.61.0
)

require (
	github.com/minio/minio-go/v7 v7.0.99
	golang.org/x/net v0.60.0
)

require (
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/klauspost/compress v1.18.2 // indirect
	github.com/klauspost/cpuid/v2 v2.2.11 // indirect
	github.com/klauspost/crc32 v1.3.0 // indirect
	github.com/minio/crc64nvme v1.1.1 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/rs/xid v1.6.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

require (
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-jose/go-jose/v4 v4.1.6 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/crypto v0.58.0
	golang.org/x/sync v0.24.0 // indirect
	modernc.org/libc v1.79.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
