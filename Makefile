# BASED OFF THE MAKEFILES PROVIDED IN PRIOR LABS
# Credit: Professor Patrick Tague

# make command will take additional argument string as MKARGS
# e.g., make test-race MKARGS="-timeout 180s"

# folder name of the package of interest
PKGNAME = orv
MKARGS = -timeout 120s
EX_EXEC = vaultkeeper

.PHONY: build all test test-race clean docs
.SILENT: build all test test-race clean docs

# build the example vaultkeeper executable
build:
	go build -C vk -o ../$(EX_EXEC) main.go

# run all tests
test:
	go test -C pkg/$(PKGNAME) -v $(MKARGS)

# run all tests
all: test build

# run all tests, but this time with the race parameter
test-race:
	go test -C pkg/$(PKGNAME) -v $(MKARGS) -race orv_test.go

# delete all executables and docs, leaving only source
clean:
	rm -r $(EX_EXEC) $(PKGNAME)-doc.txt

# generate documentation for the Orv library
docs:
	go doc -C pkg/$(PKGNAME) -u -all > $(PKGNAME)-doc.txt

