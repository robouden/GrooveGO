BINARY        := groove
BINARY_UI     := groove-ui
BINARY_WEB    := groove-web
BINARY_RELAY  := groove-relay
CMD           := ./cmd/groove
CMD_UI        := ./cmd/groove-ui
CMD_WEB       := ./cmd/groove-web
CMD_RELAY     := ./cmd/groove-relay
OUTDIR        := dist

.PHONY: all build build-ui build-web build-relay linux mac windows clean

all: linux mac windows

build:       ; go build -o $(OUTDIR)/$(BINARY) $(CMD)
build-ui:    ; go build -o $(OUTDIR)/$(BINARY_UI) $(CMD_UI)
build-web:   ; go build -o $(OUTDIR)/$(BINARY_WEB) $(CMD_WEB)
build-relay: ; go build -o $(OUTDIR)/$(BINARY_RELAY) $(CMD_RELAY)

linux:
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-linux-amd64          $(CMD)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY)-linux-arm64          $(CMD)
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_UI)-linux-amd64       $(CMD_UI)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_UI)-linux-arm64       $(CMD_UI)
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_WEB)-linux-amd64      $(CMD_WEB)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_WEB)-linux-arm64      $(CMD_WEB)
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_RELAY)-linux-amd64    $(CMD_RELAY)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_RELAY)-linux-arm64    $(CMD_RELAY)

mac:
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-mac-amd64            $(CMD)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY)-mac-arm64            $(CMD)
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_UI)-mac-amd64         $(CMD_UI)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_UI)-mac-arm64         $(CMD_UI)
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_WEB)-mac-amd64        $(CMD_WEB)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_WEB)-mac-arm64        $(CMD_WEB)
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_RELAY)-mac-amd64      $(CMD_RELAY)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_RELAY)-mac-arm64      $(CMD_RELAY)

windows:
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-windows-amd64.exe        $(CMD)
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_UI)-windows-amd64.exe     $(CMD_UI)
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_WEB)-windows-amd64.exe    $(CMD_WEB)
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_RELAY)-windows-amd64.exe  $(CMD_RELAY)

clean:
	rm -rf $(OUTDIR)
