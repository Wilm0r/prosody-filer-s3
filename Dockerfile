# First image is just for performing the build
FROM	golang:latest
WORKDIR /go/src/github.com/Wilm0r/prosody-filer/
RUN	go get -d -v github.com/BurntSushi/toml github.com/minio/minio-go
COPY	prosody-filer.go .
RUN	go build .

# Actual image will be a clean Buster image without the Golang/libs luggage.
FROM	debian:buster
RUN	apt update
RUN	apt -y install ca-certificates mime-support

RUN	useradd --uid 5280 filer
USER	filer

WORKDIR	/app
COPY --from=0 /go/src/github.com/Wilm0r/prosody-filer/prosody-filer .
ENTRYPOINT	["./prosody-filer"]  # You'll need to put/mount a config.toml here.
