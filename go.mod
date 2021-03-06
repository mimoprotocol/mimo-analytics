module github.com/mimoprotocol/mimo-analytics

go 1.14

require (
	github.com/99designs/gqlgen v0.8.3
	github.com/agnivade/levenshtein v1.0.2 // indirect
	github.com/ethereum/go-ethereum v1.8.27
	github.com/go-sql-driver/mysql v1.4.1
	github.com/golang/protobuf v1.4.2
	github.com/iotexproject/go-pkgs v0.1.2-0.20200523040337-5f1d9ddaa8ee
	github.com/iotexproject/iotex-address v0.2.2
	github.com/iotexproject/iotex-core v0.8.1-0.20200802225645-3a1451e9e968
	github.com/iotexproject/iotex-election v0.3.2
	github.com/iotexproject/iotex-proto v0.4.4-0.20200911203240-cd4805ad9b45
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v1.0.0
	github.com/rs/zerolog v1.14.3
	github.com/sergi/go-diff v1.1.0 // indirect
	github.com/stretchr/testify v1.4.0
	github.com/vektah/gqlparser v1.1.2
	go.opencensus.io v0.22.0 // indirect
	go.uber.org/zap v1.10.0
	golang.org/x/tools v0.0.0-20200318150045-ba25ddc85566 // indirect
	google.golang.org/appengine v1.6.0 // indirect
	google.golang.org/grpc v1.27.0
)

replace github.com/ethereum/go-ethereum => github.com/iotexproject/go-ethereum v0.3.0

exclude github.com/dgraph-io/badger v2.0.0-rc.2+incompatible

exclude github.com/dgraph-io/badger v2.0.0-rc2+incompatible

exclude github.com/ipfs/go-ds-badger v0.0.3
