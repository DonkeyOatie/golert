// golert handles running tests on various endpoints, checking the
// results and sending a text message if the endpoint fails the test

package main

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/boltdb/bolt"
)

var API_SECRET = os.Getenv("GOLERT_API_SECRET")
var API_USER = os.Getenv("GOLERT_API_USER")

var TWILIO_SID = os.Getenv("TWILIO_SID")
var TWILIO_TOKEN = os.Getenv("TWILIO_TOKEN")
var TWILIO_NUMBER = os.Getenv("TWILIO_NUMBER")

var ONCALL_NUMBER = os.Getenv("ONCALL_NUMBER")

const KVSTORE_FILE = "results.db"

const REQUESTS_FILE = "request_tests.json"
const TWILIO_URL = "https://api.twilio.com/2010-04-01/Accounts/%s/SMS/Messages.json"

const TEST_PASS = "pass"
const TEST_FAIL = "fail"

type AlertTest struct {
	Url        string            `json:"url"`
	StatusCode int               `json:"status_code"`
	Parameters []AlertParameters `json:"parameters"`
	Method     string            `json:"method"`
	BasicAuth  bool              `json:"basic_auth"`
}

type AlertParameters struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	EnvVar bool   `json:"env_var"`
}

func main() {
	tests := readTestsFromJson()
	runTests(tests)
}

// setResultInKVStore sets the kv value into the persistent store
func setResultInKVStore(key []byte, value []byte) {
	kvstore, _ := bolt.Open(KVSTORE_FILE, 0666, nil)
	defer kvstore.Close()
	kvstore.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("results"))
		if err != nil {
			return err
		}
		return b.Put(key, value)
	})
}

// getPreviousResultFromKVStore retrieves the result of the last run test so
// that we do not send the same failre/pass twice
func getPreviousResultFromKVStore(key []byte) string {
	var value []byte
	kvstore, _ := bolt.Open(KVSTORE_FILE, 0666, nil)
	defer kvstore.Close()
	kvstore.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("results"))
		value = b.Get(key)
		return nil
	})
	return string(value)
}

// getTestKey returns the sha1 hash of the key_string to be used as the key in
// the kvstore
func getTestKey(key_string string) []byte {
	h := sha1.New()
	io.WriteString(h, key_string)
	return h.Sum(nil)
}

// readTestsFromJson reads all of the tests in the provided file and returns a
// struct
func readTestsFromJson() []AlertTest {
	var tests []AlertTest
	alert_file, _ := os.Open(REQUESTS_FILE)
	jsonParser := json.NewDecoder(alert_file)
	if err := jsonParser.Decode(&tests); err != nil {
		fmt.Println("Failed to parse json")
		fmt.Println(err)
	}

	return tests
}

// runTests runs all of the tests in the provided list and sends an alerting
// message if any of them fail
func runTests(tests []AlertTest) {
	for _, test := range tests {
		if test.Method == "GET" {
			performGetRequest(test)
		} else if test.Method == "POST" {
			performPostRequest(test)
		}
	}
}

// performGetRequest performs a GET request with the details from the supplied
// test
func performGetRequest(test AlertTest) {
	get_url := fmt.Sprintf("%s?", test.Url)
	// iterate over all of the parameters in the json dict and append them
	// to the end of the URL
	for _, param := range test.Parameters {
		value := param.Value
		if param.EnvVar {
			value = os.Getenv(param.Value)
		}
		get_url += fmt.Sprintf("%s=%s&", param.Key, value)
	}
	req, _ := http.NewRequest("GET", get_url, nil)

	if test.BasicAuth {
		req.SetBasicAuth(API_USER, API_SECRET)
	}

	timeout := time.Duration(10 * time.Second)

	client := &http.Client{
		Timeout: timeout,
	}

	result_string := fmt.Sprintf("%s %s", test.Method, test.Url)

	res, err := client.Do(req)
	if err != nil {
		checkError(result_string, err)
		return
	}

	checkResult(result_string, res.StatusCode, test.StatusCode)
}

// performPostRequest performs a POST request with the details from the
// supplied test
func performPostRequest(test AlertTest) {
	form := url.Values{}
	// iterate over all the parameters in the json dict and add them to the
	// form that we will post
	for _, param := range test.Parameters {
		value := param.Value
		if param.EnvVar {
			value = os.Getenv(param.Value)
		}
		form.Set(param.Key, value)
	}
	req, _ := http.NewRequest("POST", test.Url, strings.NewReader(form.Encode()))
	req.Form = form
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	if test.BasicAuth {
		req.SetBasicAuth(API_USER, API_SECRET)
	}
	client := &http.Client{}
	res, _ := client.Do(req)

	result_string := fmt.Sprintf("%s %s", test.Method, test.Url)
	checkResult(result_string, res.StatusCode, test.StatusCode)
}

// checkResult compares the received and expected results and sends an alerting
// message to the oncall engineer if they differ
func checkResult(result_string string, received_code int, expected_code int) {
	if received_code != expected_code {
		msg := fmt.Sprintf(
			"%s returned %d, expected %d",
			result_string,
			received_code,
			expected_code)
		setError(result_string, msg)
	} else {
		msg := fmt.Sprintf(
			"%s is now passing",
			result_string)
		setPass(result_string, msg)
	}
}

// checkError checks the error returned by a network call and if this is the
// first failure since the last pass, sends an alert to the oncall engineer
func checkError(result_string string, err error) {
	key := getTestKey(result_string)
	if err != nil && (getPreviousResultFromKVStore(key) != TEST_FAIL) {
		setResultInKVStore(key, []byte(TEST_FAIL))
		msg := fmt.Sprintf("%s", err)
		sendAlertingMessage(msg)
	}
}

// setError sets the test result TEST_FAIL and sends an alert to the oncall
// engineer if this was the first failure since the last pass
func setError(result_string string, msg string) {
	key := getTestKey(result_string)
	// if the previous result was a failure, do not resend the text
	// message, we check != TEST_FAIL incase this was the first time the
	// tes ran
	if getPreviousResultFromKVStore(key) != TEST_FAIL {
		setResultInKVStore(key, []byte(TEST_FAIL))
		sendAlertingMessage(msg)
	}
}

// setPass sets the test result to TEST_PASS and sends an alert to the oncall
// engineer if this was the first pass since the last failure
func setPass(result_string string, msg string) {
	key := getTestKey(result_string)
	// if the previous result was a failure and we are now passing,
	// let the oncall engineer know
	if getPreviousResultFromKVStore(key) == TEST_FAIL {
		setResultInKVStore(key, []byte(TEST_PASS))
		sendAlertingMessage(msg)
	}
}

// sendAlertingMessage uses the twilio API to send a message to the on call
// phone with the details of what is broken
func sendAlertingMessage(msg string) {
	twilio_url := fmt.Sprintf(TWILIO_URL, TWILIO_SID)

	form := url.Values{}
	form.Set("From", TWILIO_NUMBER)
	form.Set("To", ONCALL_NUMBER)
	form.Set("Body", fmt.Sprintf("GOLERT ALERTING: %s", msg))

	req, _ := http.NewRequest("POST", twilio_url, strings.NewReader(form.Encode()))
	req.Form = form
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	req.SetBasicAuth(TWILIO_SID, TWILIO_TOKEN)
	client := &http.Client{}
	client.Do(req)
}
