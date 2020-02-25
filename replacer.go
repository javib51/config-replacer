package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// CmdArgs has all the command line args
type CmdArgs struct {
	workspace  string
	configFile string
}

// parseArgs parse and check the command line args
func parseArgs() *CmdArgs {
	args := new(CmdArgs)

	// Define help message format
	flag.Usage = func() {
		fmt.Printf("Usage: %s %s %s \n\n", "replacer", "-f", "<config file>")
		fmt.Printf("Fill configuration variables on config templates\n\n")
		fmt.Println("Options:")
		flag.PrintDefaults()
	}

	// Parse args
	flag.StringVar(&args.workspace, "w", ".", "Workspace directory")
	flag.StringVar(&args.configFile, "f", "", "Configuration to replace")
	flag.Parse()

	// Check mandatory args
	if args.configFile == "" {
		fmt.Printf("Error: missing args \n\n")
		flag.Usage()
		os.Exit(2)
	}

	// Convert workspace to absolute path
	baseDir, _ := os.Getwd()
	relDir, _ := filepath.Rel(baseDir, args.workspace)
	if relDir == "" {
		args.workspace = filepath.Join(baseDir, args.workspace)
	}

	return args
}

// convertYamlToMap get an unmarshal yaml and convert it into a map
func convertYamlToMap(i interface{}) interface{} {
	switch x := i.(type) {
	// if map then cast the keys into string
	case map[interface{}]interface{}:
		m2 := map[string]interface{}{}
		for k, v := range x {
			m2[k.(string)] = convertYamlToMap(v)
		}
		return m2
	// if element then iterate over the children
	case []interface{}:
		for i, v := range x {
			x[i] = convertYamlToMap(v)
		}
	}
	return i
}

// convertMapTo1DMap reduce the dimensions of a map to one
func convertMapTo1DMap(i map[string]interface{}) map[string]interface{} {
	tmp := make(map[string]interface{})

	// Reduce 1d of the map
	for k, v := range i {
		switch reflect.ValueOf(v).Kind() {
		// concatenate parent and child keys
		case reflect.Map:
			for k1, v1 := range v.(map[string]interface{}) {
				tmp[k+"."+k1] = v1
			}
		case reflect.Bool:
			tmp[k] = v
		case reflect.String:
			tmp[k] = v
		case reflect.Int:
			tmp[k] = v
		case reflect.Float32:
			tmp[k] = v
		case reflect.Float64:
			tmp[k] = v
		
		// skip other type of vars for now
		// default:
		// 	panic("Arrays are not supported")
		}
	}

	// if the new map has more dimensions then call the function again
	for _, v := range tmp {
		switch reflect.ValueOf(v).Kind() {
		case reflect.Map:
			tmp = convertMapTo1DMap(tmp)
		}
	}
	return tmp
}

// loadConfig loads a yml file with variables value
func loadConfig(args *CmdArgs) map[string]interface{} {
	// read file
	yamlFile, err := ioutil.ReadFile(args.configFile)
	if err != nil {
		msg := fmt.Sprintf("error reading %s:\n%v ", args.configFile, err)
		panic(msg)
	}

	// parse file into a map
	var body interface{}
	err = yaml.Unmarshal(yamlFile, &body)
	if err != nil {
		msg := fmt.Sprintf("error: %v", err)
		panic(msg)
	}
	cfg := convertYamlToMap(body).(map[string]interface{})

	// reduce map dimensions to 1 dimension
	cfg = convertMapTo1DMap(cfg)

	return cfg
}

func convertTypeToString(val interface{}) string {
	switch reflect.ValueOf(val).Kind() {
	case reflect.Float32:
		return strconv.FormatFloat(float64(val.(float32)), 'E', -1, 64)
	case reflect.Float64:
		return strconv.FormatFloat(val.(float64), 'E', -1, 64)
	case reflect.Int:
		return strconv.FormatInt(int64(val.(int)), 10)
	case reflect.Bool:
		return strconv.FormatBool(val.(bool))
	case reflect.String:
		return val.(string)
	default:
		panic("type is not supported")
	}

}
func replaceVarsByValues(cfg map[string]interface{}, body string) string {
	// find declared variables
	reg := regexp.MustCompile(`(?m)\{\{([^\{^\};]+)\}\}`)
	matches := reg.FindAllStringSubmatch(body, -1)

	// map variables with value
	tmpBody := body
	for _, e := range matches {
		for _, el := range e {
			if strings.Contains(el, "{{"){
				continue
			}

			if val, ok := cfg[el]; ok {
				r := regexp.MustCompile(`\{\{` + el + `\}\}`)
				tmpBody = r.ReplaceAllString(tmpBody, convertTypeToString(val))
			} else {
				msg := fmt.Sprintf("error getting variable %s: Not found\n", el)
				panic(msg)
			}
		}
	}

	return tmpBody
}

// replaceFile read a template, change the variables and create a new file
func replaceFile(args *CmdArgs, cfg map[string]interface{}, filePath string, fileName string) {
	// Read file
	file, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	// Replace vars and write new file
	var newFile bytes.Buffer
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		tmp := replaceVarsByValues(cfg, scanner.Text())
		newFile.WriteString(tmp + "\n")
	}

	if err := scanner.Err(); err != nil {
		panic(err)
	}

	// Change file name
	newName := replaceVarsByValues(cfg, fileName)
	newName = strings.Replace(newName, ".template", "", -1)

	// Get file path
	newFilePath := filepath.Join(filePath[:len(filePath)-len(fileName)], newName)

	// Save new file
	ioutil.WriteFile(newFilePath, newFile.Bytes(), 0644)

	fmt.Printf("Created %s", newFilePath)
}

// findAndReplace search for .template in the workspace and apply modifications
func findAndReplace(args *CmdArgs, cfg map[string]interface{}) {
	// Walks the file tree
	err := filepath.Walk(args.workspace, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			if strings.Contains(info.Name(), ".template") {
				replaceFile(args, cfg, path, info.Name())
				fmt.Printf("%s applied \n", info.Name())
			} else {
				fmt.Printf("%s skiped \n", info.Name())
			}
		}

		return nil
	})

	// if any error found then return error
	if err != nil {
		fmt.Printf("error walking the path %s: %v\n", args.workspace, err)
		return
	}
}

func main() {
	// parse command args
	args := parseArgs()

	// load config
	cfg := loadConfig(args)
	fmt.Println(args.workspace)

	// search for .template in the workspace and apply modifications
	findAndReplace(args, cfg)
}
