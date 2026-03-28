BINARY     := groove
BINARY_UI  := groove-ui
CMD        := ./cmd/groove
CMD_UI     := ./cmd/groove-ui
OUTDIR     := dist

.PHONY: all build build-ui linux mac windows clean

all: linux mac windows

build:
	go build -o $(OUTDIR)/$(BINARY) $(CMD)

build-ui:
	go build -o $(OUTDIR)/$(BINARY_UI) $(CMD_UI)

linux:
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-linux-amd64      $(CMD)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY)-linux-arm64      $(CMD)
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_UI)-linux-amd64   $(CMD_UI)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_UI)-linux-arm64   $(CMD_UI)

mac:
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-mac-amd64        $(CMD)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY)-mac-arm64        $(CMD)
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_UI)-mac-amd64     $(CMD_UI)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY_UI)-mac-arm64     $(CMD_UI)

windows:
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-windows-amd64.exe    $(CMD)
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY_UI)-windows-amd64.exe $(CMD_UI)

clean:
	rm -rf $(OUTDIR)
