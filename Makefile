# full pkg name
PKG = gin-data-checker

# Binary
APP = annexcheck

# Build loc
BUILDLOC = build

# Install location
INSTLOC = $(GOPATH)/bin

# Build flags
VERNUM = $(shell cut -d= -f2 version)
ncommits = $(shell git rev-list --count HEAD)
BUILDNUM = $(shell printf '%06d' $(ncommits))
COMMITHASH = $(shell git rev-parse HEAD)
LDFLAGS = -ldflags="-X main.appversion=$(VERNUM) -X main.build=$(BUILDNUM) -X main.commit=$(COMMITHASH)"

SOURCES = $(shell find . -type f -iname "*.go") version

.PHONY: $(APP) install clean uninstall

$(APP): $(BUILDLOC)/$(APP)

install: $(APP)
	install $(BUILDLOC)/$(APP) $(INSTLOC)/$(APP)

clean:
	rm -r $(BUILDLOC)

uninstall:
	rm $(INSTLOC)/$(APP)

$(BUILDLOC)/$(APP): $(SOURCES)
	go build $(LDFLAGS) -o $(BUILDLOC)/$(APP) ./cmd/annexcheck
