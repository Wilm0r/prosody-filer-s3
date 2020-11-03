package main

/*
 * Manual testing with CURL
 * Send with:
 * curl -X PUT "http://localhost:5050/upload/thomas/abc/catmetal.jpg?v=e17531b1e88bc9a5cbf816eca8a82fc09969c9245250f3e1b2e473bb564e4be0" --data-binary '@catmetal.jpg'
 * HMAC: e17531b1e88bc9a5cbf816eca8a82fc09969c9245250f3e1b2e473bb564e4be0
 */

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	minio "github.com/minio/minio-go"
)

func mockUpload() {
	_, err := s3Client.FPutObject(context.Background(), conf.S3Bucket, "/thomas/abc/catmetal.jpg", "./catmetal.jpg", minio.PutObjectOptions{})
	if err != nil {
		log.Fatal(err)
	}
}

func cleanup() {
	err := s3Client.RemoveObject(context.Background(), conf.S3Bucket, "/thomas/abc/catmetal.jpg", minio.RemoveObjectOptions{})
	if err != nil {
		log.Fatal(err)
	}
}

func TestReadConfig(t *testing.T) {
	// Set config
	err := readConfig("config.toml", &conf)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUploadValid(t *testing.T) {
	// Set config
	readConfig("config.toml", &conf)
	s3Login()

	// Read catmetal file
	catmetalfile, err := ioutil.ReadFile("catmetal.jpg")
	if err != nil {
		t.Fatal(err)
	}

	// Create request
	req, err := http.NewRequest("PUT", "/upload/thomas/abc/catmetal.jpg", bytes.NewBuffer(catmetalfile))
	q := req.URL.Query()
	q.Add("v", "1924ba5c934977747c91039b772b460664e5cee4104ae85c31449114ad194cfa")
	req.URL.RawQuery = q.Encode()

	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(handleRequest)

	// Send request and record response
	handler.ServeHTTP(rr, req)

	// Check status code
	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v. HTTP body: %s", status, http.StatusOK, rr.Body.String())
	}

	// clean up
	cleanup()
}

func TestUploadMissingMAC(t *testing.T) {
	// Set config
	readConfig("config.toml", &conf)
	s3Login()

	// Read catmetal file
	catmetalfile, err := ioutil.ReadFile("catmetal.jpg")
	if err != nil {
		t.Fatal(err)
	}

	// Create request
	req, err := http.NewRequest("PUT", "/upload/thomas/abc/catmetal.jpg", bytes.NewBuffer(catmetalfile))

	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(handleRequest)

	// Send request and record response
	handler.ServeHTTP(rr, req)

	// Check status code
	if status := rr.Code; status != http.StatusForbidden {
		t.Errorf("handler returned wrong status code: got %v want %v. HTTP body: %s", status, http.StatusForbidden, rr.Body.String())
	}
}

func TestUploadInvalidMAC(t *testing.T) {
	// Set config
	readConfig("config.toml", &conf)
	s3Login()

	// Read catmetal file
	catmetalfile, err := ioutil.ReadFile("catmetal.jpg")
	if err != nil {
		t.Fatal(err)
	}

	// Create request
	req, err := http.NewRequest("PUT", "/upload/thomas/abc/catmetal.jpg", bytes.NewBuffer(catmetalfile))
	q := req.URL.Query()
	q.Add("v", "abc")
	req.URL.RawQuery = q.Encode()

	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(handleRequest)

	// Send request and record response
	handler.ServeHTTP(rr, req)

	// Check status code
	if status := rr.Code; status != http.StatusForbidden {
		t.Errorf("handler returned wrong status code: got %v want %v. HTTP body: %s", status, http.StatusForbidden, rr.Body.String())
	}
}

func TestUploadInvalidMethod(t *testing.T) {
	// Set config
	readConfig("config.toml", &conf)
	s3Login()

	// Read catmetal file
	catmetalfile, err := ioutil.ReadFile("catmetal.jpg")
	if err != nil {
		t.Fatal(err)
	}

	// Create request
	req, err := http.NewRequest("POST", "/upload/thomas/abc/catmetal.jpg", bytes.NewBuffer(catmetalfile))

	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(handleRequest)

	// Send request and record response
	handler.ServeHTTP(rr, req)

	// Check status code
	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code: got %v want %v. HTTP body: %s", status, http.StatusMethodNotAllowed, rr.Body.String())
	}
}

func TestDownloadOK(t *testing.T) {
	// Set config
	readConfig("config.toml", &conf)
	s3Login()

	// Mock upload
	mockUpload()

	for _, method := range []string{"GET", "HEAD"} {
		for _, proxy := range []bool{false, true} {
			conf.ProxyMode = proxy
			t.Run(fmt.Sprintf("method %s proxy %t", method, proxy), func(t *testing.T) {
				// Create request
				req, err := http.NewRequest("HEAD", "/upload/thomas/abc/catmetal.jpg", nil)

				if err != nil {
					t.Fatal(err)
				}

				rr := httptest.NewRecorder()
				handler := http.HandlerFunc(handleRequest)

				// Send request and record response
				handler.ServeHTTP(rr, req)

				// Check status code
				var wanted = http.StatusFound
				if proxy {
					wanted = http.StatusOK
				}
				if status := rr.Code; status != wanted {
					t.Errorf("handler returned wrong status code: got %v want %v. HTTP body: %s", status, wanted, rr.Body.String())
				}
			})
		}
	}

	// cleanup
	cleanup()
}

func TestEmptyGet(t *testing.T) {
	// Set config
	readConfig("config.toml", &conf)
	s3Login()

	// Create request
	req, err := http.NewRequest("GET", "", nil)

	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(handleRequest)

	// Send request and record response
	handler.ServeHTTP(rr, req)

	// Check status code
	if status := rr.Code; status != http.StatusBadGateway {
		t.Errorf("handler returned wrong status code: got %v want %v. HTTP body: %s", status, http.StatusBadGateway, rr.Body.String())
	}
}
