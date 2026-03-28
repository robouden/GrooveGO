BINARY      := groove
BINARY_UI   := groove-ui
BINARY_WEB  := groove-web
CMD         := ./cmd/groove
CMD_UI      := ./cmd/groove-ui
CMD_WEB     := ./cmd/groove-web
OUTDIR      := dist

.PHONY: all build build-ui build-web linux mac windows clean

all: linux mac windows

build:
	go build -o $(OUTDIR)/$(BINARY) $(CMD)

build-ui:
	go build -o $(OUTDIR)/$(BINARY_UI) $(CMD_UI)

build-web:
	go build -o $(OUTDIR)/$(BINARY_WEB) $(CMD_WEB)

linux:
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-linux-amd64       $(CMD)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY)-linux-arm64       $(CMD)
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_UI)-linux-amd64    $(CMD_UI)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_UI)-linux-arm64    $(CMD_UI)
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_WEB)-linux-amd64   $(CMD_WEB)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_WEB)-linux-arm64   $(CMD_WEB)

mac:
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-mac-amd64         $(CMD)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY)-mac-arm64         $(CMD)
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_UI)-mac-amd64      $(CMD_UI)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_UI)-mac-arm64      $(CMD_UI)
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_WEB)-mac-amd64     $(CMD_WEB)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_WEB)-mac-arm64     $(CMD_WEB)

windows:
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-windows-amd64.exe     $(CMD)
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_UI)-windows-amd64.exe  $(CMD_UI)
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_WEB)-windows-amd64.exe $(CMD_WEB)

clean:
	rm -rf $(OUTDIR)
