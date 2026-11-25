module github.com/ly-solutions/harnessly

go 1.26.0

toolchain go1.26.11

// The front-end's npm packages may carry Go files of their own.
ignore ./web/node_modules

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/coder/websocket v1.8.15
	github.com/creack/pty v1.1.24
	github.com/dlclark/regexp2 v1.12.0
	github.com/emersion/go-imap v1.2.1
	github.com/emersion/go-message v0.18.2
	github.com/emersion/go-sasl v0.0.0-20241020182733-b788ff22d5a6
	github.com/emersion/go-smtp v0.26.0
	github.com/evanw/esbuild v0.28.2
	github.com/fsnotify/fsnotify v1.10.1
	github.com/segmentio/asm v1.2.1
	github.com/yuin/goldmark v1.8.7
	golang.org/x/text v0.44.0
)

require golang.org/x/sys v0.50.0

require (
	github.com/andybalholm/brotli v1.3.0
	github.com/coreos/go-oidc/v3 v3.21.0
	github.com/go-webauthn/webauthn v0.18.2
	github.com/jackc/pgx/v5 v5.13.0
	golang.org/x/oauth2 v0.38.0
	modernc.org/sqlite v1.62.0
)

require (
	filippo.io/age v1.3.2
	github.com/emersion/go-ical v0.0.0-20240127095438-fc1c9d8fb2b6
	github.com/gotd/td v0.166.1
	github.com/minio/minio-go/v7 v7.4.1
	github.com/pion/webrtc/v4 v4.2.27
	github.com/teambition/rrule-go v1.8.2
	go.yaml.in/yaml/v3 v3.0.6
	golang.org/x/net v0.63.0
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	rsc.io/qr v0.2.0
)

require (
	filippo.io/hpke v0.4.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-faster/errors v0.8.0 // indirect
	github.com/go-faster/jx v1.2.0 // indirect
	github.com/go-faster/xor v1.0.0 // indirect
	github.com/go-faster/yaml v0.4.6 // indirect
	github.com/gotd/ige v0.3.0 // indirect
	github.com/gotd/log v0.1.0 // indirect
	github.com/gotd/neo v0.1.5 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/klauspost/crc32 v1.3.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/minio/crc64nvme v1.1.1 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/ogen-go/ogen v1.23.0 // indirect
	github.com/pion/datachannel v1.6.4 // indirect
	github.com/pion/dtls/v3 v3.1.16 // indirect
	github.com/pion/ice/v4 v4.4.9 // indirect
	github.com/pion/interceptor v0.1.51 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.2.2 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.20 // indirect
	github.com/pion/rtp v1.10.6 // indirect
	github.com/pion/sctp v1.12.1 // indirect
	github.com/pion/sdp/v3 v3.0.20 // indirect
	github.com/pion/srtp/v3 v3.1.3 // indirect
	github.com/pion/stun/v4 v4.0.1 // indirect
	github.com/pion/transport/v5 v5.1.5 // indirect
	github.com/pion/turn/v5 v5.1.3 // indirect
	github.com/refraction-networking/utls v1.8.2 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	golang.org/x/exp v0.0.0-20230725093048-515e97ebf090 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	gopkg.in/ini.v1 v1.67.3 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

require (
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-jose/go-jose/v4 v4.1.9
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/crypto v0.59.0
	golang.org/x/sync v0.25.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
