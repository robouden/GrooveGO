BINARY  := groove
MODULE  := github.com/safecast/groove-go
CMD     := ./cmd/groove
OUTDIR  := dist

.PHONY: all build linux mac windows clean

all: linux mac windows

build:
	go build -o $(OUTDIR)/$(BINARY) $(CMD)

linux:
	GOOS=linux   GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-linux-amd64   $(CMD)
	GOOS=linux   GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY)-linux-arm64   $(CMD)

mac:
	GOOS=darwin  GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-mac-amd64     $(CMD)
	GOOS=darwin  GOARCH=arm64 go build -o $(OUTDIR)/$(BINARY)-mac-arm64     $(CMD)

windows:
	GOOS=windows GOARCH=amd64 go build -o $(OUTDIR)/$(BINARY)-windows-amd64.exe $(CMD)

clean:
	rm -rf $(OUTDIR)
