package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// MAXFILESIZE is the maximum size of file to be read
// If env variable GOT_TRIM=XXX, read last XXX bytes of file
const MAXFILESIZE int64 = 10000000

// Options to connect to goterra
type Options struct {
	url        *string
	deployment *string
	token      *string
}

// DeploymentData represents data sent to update a deployment value
type DeploymentData struct {
	Key   string
	Value string
}

// Deployment gets info of a new deployment
type Deployment struct {
	URL   string `json:"url"`
	ID    string `json:"id"`
	Token string `json:"token"`
}

// Version is the current version of software
var Version string

func getValue(options Options, key string) bool {
	client := &http.Client{}
	remote := []string{*options.url, "store", *options.deployment, key}
	req, _ := http.NewRequest("GET", strings.Join(remote, "/"), nil)
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", *options.token))
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("failed to contact server %s\n", *options.url)
		return true
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return true
	}
	respData := &DeploymentData{}
	json.NewDecoder(resp.Body).Decode(respData)
	fmt.Printf("%s\n", respData.Value)
	return false

}

func putValue(options Options, key string, value string) bool {
	client := &http.Client{}
	dataToSet := value
	if strings.HasPrefix(value, "@") {
		// this is a file
		filePath := strings.Replace(value, "@", "", 1)
		stat, err := os.Stat(filePath)
		fileData := make([]byte, 0)
		if err != nil {
			fmt.Printf("could not read file %s, %s", value, err)
			return false
		}
		// Arbitrary max size
		if stat.Size() > MAXFILESIZE || os.Getenv("GOT_TRIM") != "" {
			trim := float64(MAXFILESIZE)
			if os.Getenv("GOT_TRIM") != "" {
				var trimErr error
				trim, trimErr = strconv.ParseFloat(os.Getenv("GOT_TRIM"), 64)
				if trimErr != nil {
					trim = float64(MAXFILESIZE)
				}
			}
			max := math.Min(float64(MAXFILESIZE), trim)
			fmt.Printf("read only partial file %s: last %f bytes", filePath, max)

			buf := make([]byte, int64(max))
			start := stat.Size() - int64(max)
			if start < 0 {
				// trying to trim, but file is lower than trim value, take whole file
				start = 0
				buf = make([]byte, stat.Size())
			}
			file, fileErr := os.Open(filePath)
			if fileErr != nil {
				fmt.Printf("could not read file %s, %s", value, fileErr)
				return false
			}
			_, readErr := file.ReadAt(buf, start)
			if err == nil {
				fileData = buf
			} else {
				fmt.Printf("could not read file %s, %s", value, readErr)
				return false
			}
		} else {
			fileData, err = ioutil.ReadFile(filePath)
			if err != nil {
				fmt.Printf("could not read file %s, %s", value, err)
				return false
			}
		}
		dataToSet = fmt.Sprintf("%s", fileData)
	}
	data := &DeploymentData{Key: key, Value: dataToSet}
	jsonValue, _ := json.Marshal(data)
	jsonData := bytes.NewBuffer(jsonValue)
	remote := []string{*options.url, "store", *options.deployment}
	req, _ := http.NewRequest("PUT", strings.Join(remote, "/"), jsonData)
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", *options.token))
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("failed to contact server %s\n", *options.url)
		return true
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Printf("failed to update deployment %d\n", resp.StatusCode)
		return true
	}
	return false
}

func create(options Options) bool {
	client := &http.Client{}
	remote := []string{*options.url, "store"}
	byteData := make([]byte, 0)
	req, _ := http.NewRequest("POST", strings.Join(remote, "/"), bytes.NewReader(byteData))
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("failed to contact server %s\n", *options.url)
		return true
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Printf("failed to create deployment %d\n", resp.StatusCode)
		return true
	}
	respData := &Deployment{}
	json.NewDecoder(resp.Body).Decode(respData)
	fmt.Printf("url=%s\n", respData.URL)
	fmt.Printf("id=%s\n", respData.ID)
	fmt.Printf("token=%s\n", respData.Token)
	return false

}

func main() {
	var helpVersion = false
	var options = Options{}
	var timeout uint
	flag.BoolVar(&helpVersion, "version", false, "Show version")
	flag.UintVar(&timeout, "timeout", 30, "on *get* , expires after timeout minutes, else wait")
	options.url = flag.String("url", os.Getenv("GOT_URL"), "goterra url")
	options.deployment = flag.String("deployment", os.Getenv("GOT_DEPLOYMENT"), "deployment id")
	options.token = flag.String("token", os.Getenv("GOT_TOKEN"), "deployment token")

	var CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	cmdHelp := `
Position arguments:
  <subcommand>
	create					Create a new deployment
	get						Get a deployment value
	put						Set a deployment value

Examples:

  goterra-cli create
  goterra-cli get key1
  goterra-cli put key1 value1
	`
	flag.Usage = func() {
		fmt.Fprintf(CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(CommandLine.Output(), "%s [options] <subcommand> [commandoptions]\n", os.Args[0])
		fmt.Fprintf(CommandLine.Output(), "%s\n", cmdHelp)
		// TODO other commands
		fmt.Fprintf(CommandLine.Output(), "Optional arguments\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if helpVersion {
		fmt.Printf("Version: %s\n", Version)
		return
	}

	tail := flag.Args()
	lenTail := len(tail)
	if lenTail == 0 {
		fmt.Printf("No command specified\n")
		flag.PrintDefaults()
		return
	}

	err := false
	switch tail[0] {
	case "get":
		key := strings.TrimSpace(tail[1])
		err := true
		now := time.Now()
		timeoutAt := time.Now().Add(time.Duration(timeout) * time.Minute)
		for err {
			err = getValue(options, key)
			if err {
				// Could not fetch, check if timeout expired,
				// else sleep and try again
				now = time.Now()
				if now.After(timeoutAt) {
					// timeout reached, fail
					err = false
					fmt.Printf("failed to get deployment key %s\n", key)
				} else {
					time.Sleep(5 * time.Second)
				}
			}
		}

	case "put":
		key := strings.TrimSpace(tail[1])
		value := strings.TrimSpace(tail[2])
		err = putValue(options, key, value)

	case "create":
		err = create(options)
	}

	if err {
		os.Exit(1)
	}
	os.Exit(0)

}
