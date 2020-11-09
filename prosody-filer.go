/*
 * This module allows upload via mod_http_upload_external
 * Also see: https://modules.prosody.im/mod_http_upload_external.html
 */

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	minio "github.com/minio/minio-go"
	"github.com/minio/minio-go/pkg/credentials"
)

/*
 * Configuration of this server
 */
type Config struct {
	Listenport   string
	Secret       string
	UploadSubDir string

	ProxyMode bool

	S3Endpoint  string
	S3AccessKey string
	S3Secret    string
	S3TLS       bool
	S3Bucket    string
}

var conf Config
var s3Client *minio.Client

const ALLOWED_METHODS string = "OPTIONS, HEAD, GET, PUT"

/*
 * Sets CORS headers
 */
func addCORSheaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", ALLOWED_METHODS)
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Max-Age", "7200")
}

func addContentHeaders(h http.Header, filename string) {
	ctype := mime.TypeByExtension(filepath.Ext(filename))
	h.Set("Content-Type", ctype)
	if m, _ := regexp.MatchString("((audio|image|video)/.*|text/plain)", ctype); m {
		h.Set("Content-Disposition", "inline")
	} else {
		h.Set("Content-Disposition", "attachment")
	}
}

/*
 * Request handler
 * Is activated when a clients requests the file, file information or an upload
 */
func handleRequest(w http.ResponseWriter, r *http.Request) {
	log.Println("Incoming request:", r.Method, r.URL.String())

	// Parse URL and args
	u, err := url.Parse(r.URL.String())
	if err != nil {
		log.Println("Failed to parse URL:", err)
	}

	a, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		log.Println("Failed to parse URL query params:", err)
	}

	fileStorePath := strings.TrimPrefix(u.Path, "/"+conf.UploadSubDir)

	// Add CORS headers
	addCORSheaders(w)

	if r.Method == "PUT" {
		// Check if MAC is attached to URL
		if a["v"] == nil {
			log.Println("Error: No HMAC attached to URL.")
			http.Error(w, "Needs HMAC", 403)
			return
		}

		/*
		 * Check if the request is valid
		 */
		mac := hmac.New(sha256.New, []byte(conf.Secret))
		log.Println("fileStorePath:", fileStorePath)
		log.Println("ContentLength:", strconv.FormatInt(r.ContentLength, 10))
		mac.Write([]byte(fileStorePath + " " + strconv.FormatInt(r.ContentLength, 10)))
		macString := hex.EncodeToString(mac.Sum(nil))

		/*
		 * Check whether calculated (expected) MAC is the MAC that client send in "v" URL parameter
		 */
		if !hmac.Equal([]byte(macString), []byte(a["v"][0])) {
			log.Println("Invalid MAC, expected:", macString)
			http.Error(w, "403 Forbidden", 403)
			return
		}

		ch := make(http.Header)
		addContentHeaders(ch, fileStorePath)

		// Somewhat redundant since we're setting these in the signed URL as well, but why not?
		var opt minio.PutObjectOptions
		opt.ContentType = ch.Get("Content-Type")
		opt.ContentDisposition = ch.Get("Content-Disposition")

		s3file, err := s3Client.PutObject(context.Background(), conf.S3Bucket, fileStorePath, r.Body, r.ContentLength, minio.PutObjectOptions{})
		if err != nil {
			log.Println("Uploading file failed:", err)
			http.Error(w, "Backend Error", 502)
			return
		}

		log.Println("Successfully stored file with ETag", s3file.ETag)
		w.WriteHeader(http.StatusCreated)
	} else if r.Method == "HEAD" || r.Method == "GET" {
		if conf.ProxyMode {
			obj, err := s3Client.GetObject(context.Background(), conf.S3Bucket, fileStorePath, minio.GetObjectOptions{})
			if err != nil {
				log.Println("Storage error:", err)
				http.Error(w, "Storage error", 502)
				return
			}
			addContentHeaders(w.Header(), fileStorePath)
			// Content-Length for HEAD?
			if r.Method == "GET" {
				http.ServeContent(w, r, fileStorePath, time.Now(), obj)
			}
		} else {
			ch := make(http.Header)
			addContentHeaders(ch, fileStorePath)
			uv := make(url.Values)
			for k, v := range ch {
				uv.Set("response-"+strings.ToLower(k), v[0])
			}

			// NOTE: This is an offline operation, using just our credentials, so it'll work for any URL,
			// it's up to the S3 backend to 404 if the file isn't there.
			url, err := s3Client.PresignedGetObject(context.Background(), conf.S3Bucket, fileStorePath, 24*time.Hour, uv)
			if err != nil {
				log.Println("Storage error:", err)
				http.Error(w, "Storage error", 502)
				return
			}

			w.Header().Set("Location", url.String())
			w.WriteHeader(http.StatusFound) // better known as 302
		}
	} else if r.Method == "OPTIONS" {
		w.Header().Set("Allow", ALLOWED_METHODS)
		return
	} else {
		log.Println("Invalid method", r.Method)
		http.Error(w, "405 Method Not Allowed", 405)
		return
	}
}

func readConfig(configfilename string, conf *Config) error {
	log.Println("Reading configuration ...")

	conf.S3TLS = true

	configdata, err := ioutil.ReadFile(configfilename)
	if err != nil {
		log.Fatal("Configuration file config.toml cannot be read:", err, "...Exiting.")
		return err
	}

	if _, err := toml.Decode(string(configdata), conf); err != nil {
		log.Fatal("Config file config.toml is invalid:", err)
		return err
	}

	// Support standard AWS credential env variables as well (will override whatever may have been in the config!)
	if key, has := os.LookupEnv("AWS_ACCESS_KEY_ID"); has {
		log.Println("Loading AWS credentials from evironment instead of config")
		conf.S3AccessKey = key
	}
	if key, has := os.LookupEnv("AWS_SECRET_ACCESS_KEY"); has {
		conf.S3Secret = key
	}
	return nil
}

func s3Login() {
	var err error
	s3Client, err = minio.New(conf.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(conf.S3AccessKey, conf.S3Secret, ""),
		Secure: conf.S3TLS,
	})
	if err != nil {
		log.Fatalln(err)
	}
	exists, err := s3Client.BucketExists(context.Background(), conf.S3Bucket)
	if err != nil {
		log.Fatalln(err)
	}
	if !exists {
		// Buggy example: Scaleway, appears to always report non-existent.
		// But hey at least we've verified that the credentials work which is actually the main thing I want to check here.
		log.Println("WARNING: Bucket does not exist (or S3 service is buggy): " + conf.S3Bucket)
	}
}

/*
 * Main function
 */
func main() {
	/*
	 * Read startup arguments
	 */
	var argConfigFile = flag.String("config", "./config.toml", "Path to configuration file \"config.toml\".")
	flag.Parse()

	/*
	 * Read config file
	 */
	err := readConfig(*argConfigFile, &conf)
	if err != nil {
		log.Println("There was an error while reading the configuration file:", err)
	}

	log.Println("Starting Prosody-Filer-S3...")
	s3Login()
	log.Println("S3 bucket found.")

	/*
	 * Start HTTP server
	 */
	http.HandleFunc("/"+conf.UploadSubDir, handleRequest)
	log.Printf("Server started on %s. Waiting for requests.\n", conf.Listenport)
	err = http.ListenAndServe(conf.Listenport, nil)
	if err != nil {
		log.Fatalln(err)
	}
}
