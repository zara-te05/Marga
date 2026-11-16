# Atajos para Linux/macOS (en Windows usa los comandos de go directamente).
.PHONY: compilar probar probar-integracion probar-e2e probar-todo css servidor bd

BD_PRUEBAS ?= postgres://marga:marga@localhost:5432/marga?sslmode=disable

compilar:            ## compila marga y marga-server en bin/
	go build -o bin/ ./cmd/...

probar:              ## pruebas unitarias: tests/unit (sin base de datos)
	go test ./tests/unit/...

probar-integracion:  ## tests/integracion: necesita PostgreSQL (make bd)
	MARGA_TEST_DB_URL=$${MARGA_TEST_DB_URL:-$(BD_PRUEBAS)} go test -count=1 ./tests/integracion/...

probar-e2e:          ## tests/e2e: la CLI completa contra un servidor real; necesita PostgreSQL
	MARGA_TEST_DB_URL=$${MARGA_TEST_DB_URL:-$(BD_PRUEBAS)} go test -count=1 ./tests/e2e/...

probar-todo:         ## las tres
	MARGA_TEST_DB_URL=$${MARGA_TEST_DB_URL:-$(BD_PRUEBAS)} go test -count=1 ./...

css:                 ## regenera internal/vistas/web/assets/css/marga.css
	cd internal/vistas/web && npm install --no-audit --no-fund && npm run css

servidor:            ## servidor con datos demo y el front leído de disco
	MARGA_WEB_DIR=./internal/vistas/web go run ./cmd/marga-server -seed

bd:                  ## PostgreSQL de desarrollo en Docker
	docker compose -f docker-compose.dev.yml up -d
