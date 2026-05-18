module languagetool-backend

go 1.22.2

require (
	github.com/alicebob/gopher-json v0.0.0-20230218143504-906a9b012302
	github.com/alicebob/miniredis/v2 v2.34.0
	github.com/bsm/ginkgo/v2 v2.12.0
	github.com/bsm/gomega v1.27.10
	github.com/cespare/xxhash/v2 v2.2.0
	github.com/davecgh/go-spew v1.1.1
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f
	github.com/go-chi/chi/v5 v5.2.1
	github.com/gorilla/websocket v1.5.3
	github.com/pmezard/go-difflib v1.0.0
	github.com/redis/go-redis/v9 v9.7.3
	github.com/stretchr/testify v1.9.0
	github.com/yuin/gopher-lua v1.1.1
	gopkg.in/check.v1 v0.0.0-20161208181325-20d25e280405
	gopkg.in/yaml.v3 v3.0.1
)

replace gopkg.in/yaml.v3 => ./vendor-local/yaml.v3
replace gopkg.in/check.v1 => ./vendor-local/check.v1
