package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gookit/color"
	"github.com/pkg/errors"
	"github.com/robertkrimen/otto"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TestGroup struct {
	Package string
	Base    string
	Tests   map[string]*Test

	FailCount int
	wg        sync.WaitGroup
}

type Test struct {
	Name    string
	After   string
	Request struct {
		Method      string
		Path        string
		Headers     map[string]string
		QueryParams interface{}
		Body        interface{}
	}
	Validation struct {
		StatusCode string
		Headers    map[string]string
		Body       []string
	}

	Result struct {
		Passed bool
		Errors []error
	}
}

var TestQueue = make(chan *TestNode)

// Main entrypoint
func run() {
	// Run tests as they're queued
	go func() {
		for {
			node := <-TestQueue
			go runTest(TestQueue, node)
		}
	}()

	var configsWg sync.WaitGroup
	configs := findConfigs()
	for i := range configs {
		configsWg.Add(1)
		go processConfig(&configsWg, &configs[i])
	}

	configsWg.Wait()

	totalFails := 0
	for _, config := range configs {
		totalFails += config.FailCount
	}

	os.Exit(totalFails)
}

func processConfig(configWg *sync.WaitGroup, config *TestGroup) {
	defer configWg.Done()

	// Create a node for each test
	nodes := make(map[string]*TestNode)
	for key, test := range config.Tests {
		test.Name = key
		nodes[key] = &TestNode{Key: key, Test: test, Config: config}
	}

	// Construct "graph" of nodes based on test dependencies
	var rootNodes []*TestNode
	for _, node := range nodes {
		config.wg.Add(1)
		if node.Test.After == "" {
			rootNodes = append(rootNodes, node)
		} else {
			nodes[node.Test.After].Children = append(nodes[node.Test.After].Children, node)
		}
	}

	// Queue tests with no "dependencies"
	for _, node := range rootNodes {
		TestQueue <- node
	}

	config.wg.Wait()
	printSummary(config)
	printDetails(config)
	fmt.Println()
}

// Recursively walk down from the current directory to find test configs
func findConfigs() []TestGroup {
	var configs []TestGroup
	err := filepath.Walk(".",
		func(path string, _ os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if filepath.Base(path) != "yest.yml" {
				return nil
			}

			var config TestGroup
			parent := strings.Replace(path, "yest.yml", "", 1) // TODO: A slice would be more efficient
			parent = strings.TrimSuffix(parent, "/")
			if parent == "" { // handle root case
				wd, _ := os.Getwd()
				parent = filepath.Base(wd)
			}
			config.Package = parent

			data, err := ioutil.ReadFile(path)
			if err != nil {
				log.Println(err)
				return nil
			}
			err = yaml.Unmarshal(data, &config)
			if err != nil {
				log.Println(err)
				return nil
			}

			configs = append(configs, config)
			return nil
		})
	if err != nil {
		log.Println(err)
	}

	return configs
}

// Print the testing summary
func printSummary(config *TestGroup) {
	total := len(config.Tests)
	passed := total - config.FailCount

	fmt.Printf("== [%s] Result Summary ==\n", config.Package)
	if config.FailCount == 0 {
		color.Green.Printf("%d/%d Tests Passed\n", passed, total)
	} else if config.FailCount == total {
		color.Red.Printf("%d/%d Tests Passed\n", passed, total)
	} else {
		color.Yellow.Printf("%d/%d Tests Passed\n", passed, total)
	}
}

// Print any errors that arose during testing
func printDetails(config *TestGroup) {
	for _, test := range config.Tests {
		if test.Result.Passed {
			if verbose {
				color.Green.Print("PASSED")
				fmt.Printf(" [%s]\n", test.Name)
			}

			continue
		}

		color.Red.Print("FAILED")
		padding := " "
		for _, err := range test.Result.Errors {
			fmt.Printf("%s[%s]: %s\n", padding, test.Name, err)
			padding = "       "
		}
		fmt.Println()
	}
}

// Run a single test, using goroutines to parallelize
func runTest(q chan *TestNode, node *TestNode) {
	test := node.Test
	config := node.Config
	defer config.wg.Done()

	// -- Prepare query
	if test.Request.Method == "" { // default to GET
		test.Request.Method = "GET"
	}

	var body []byte
	if test.Request.Body != nil {
		var err error
		body, err = json.Marshal(test.Request.Body)
		if err != nil {
			fmt.Println("INTERNAL ERROR")
			test.Result.Errors = append(test.Result.Errors, err)
			return
		}
	}

	req, err := http.NewRequest(test.Request.Method, config.Base+test.Request.Path, bytes.NewBuffer(body))
	if err != nil {
		test.Result.Errors = append(test.Result.Errors, err)
		return
	}

	for k, v := range test.Request.Headers {
		req.Header.Set(k, v)
	}

	// -- Execute query
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		test.Result.Errors = append(test.Result.Errors, err)
		return
	}

	// -- Validation
	v := test.Validation

	// Status Code
	if v.StatusCode != "" && strconv.Itoa(resp.StatusCode) != v.StatusCode {
		e := errors.New(fmt.Sprintf("expected status code: %s, actual: %d", v.StatusCode, resp.StatusCode))
		test.Result.Errors = append(test.Result.Errors, e)
	}

	// Headers
	for header, expected := range v.Headers {
		actual := resp.Header.Get(header)
		if actual != expected {
			e := errors.New(fmt.Sprintf("expected header %s: %s, actual: %s", header, expected, actual))
			test.Result.Errors = append(test.Result.Errors, e)
		}
	}

	// Body
	if test.Validation.Body != nil {
		// TODO: probably don't ignore this error lol
		bodyString, _ := ioutil.ReadAll(resp.Body)
		var body interface{}
		_ = json.Unmarshal(bodyString, &body)

		// start a Javascript interpreter and checks each expression in the YAML config
		vm := otto.New()
		err = vm.Set("body", body)
		if err != nil {
			log.Fatal(err)
		}

		for _, expr := range test.Validation.Body {
			_, err = vm.Run(fmt.Sprintf("result = %s", expr))
			if err != nil {
				e := errors.New(fmt.Sprintf("(%s) evaluated with error: %s", expr, err))
				test.Result.Errors = append(test.Result.Errors, e)
				continue
			}

			value, err := vm.Get("result")
			if err != nil {
				log.Fatal(err) // TODO: this maybe shouldn't be this.
			}

			result, _ := value.ToBoolean()
			if !result {
				e := errors.New(fmt.Sprintf("(%s) evaluated to false", expr))
				test.Result.Errors = append(test.Result.Errors, e)
			}
		}
	}

	test.Result.Passed = len(test.Result.Errors) == 0
	if !test.Result.Passed {
		config.FailCount++
	}

	// Enqueue dependent tests
	time.Sleep(100 * time.Millisecond) // Sleep between successive tests. TODO: Make configurable
	for _, child := range node.Children {
		q <- child
	}
}
