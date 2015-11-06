build: clean
	goimports -w .
	go get .
	go build
	strip golert

clean:
	-rm golert
