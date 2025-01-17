package goflags

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/cnf/structhash"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

// FlagSet is a list of flags for an application
type FlagSet struct {
	Marshal     bool
	description string
	flagKeys    InsertionOrderedMap
}

type flagData struct {
	usage        string
	short        string
	long         string
	defaultValue interface{}
}

// NewFlagSet creates a new flagSet structure for the application
func NewFlagSet() *FlagSet {
	return &FlagSet{flagKeys: *newInsertionOrderedMap()}
}

func newInsertionOrderedMap() *InsertionOrderedMap {
	return &InsertionOrderedMap{
		values: make(map[string]*flagData),
		keys:   make([]string, 0, 0),
	}
}

// Hash returns the unique hash for a flagData structure
// NOTE: Hash panics when the structure cannot be hashed.
func (flagSet *flagData) Hash() string {
	hash, _ := structhash.Hash(flagSet, 1)
	return hash
}

// SetDescription sets the description field for a flagSet to a value.
func (flagSet *FlagSet) SetDescription(description string) {
	flagSet.description = description
}

// MergeConfigFile reads a config file to merge values from.
func (flagSet *FlagSet) MergeConfigFile(file string) error {
	return flagSet.readConfigFile(file)
}

// Parse parses the flags provided to the library.
func (flagSet *FlagSet) Parse() error {
	flag.CommandLine.Usage = flagSet.usageFunc
	flag.Parse()

	appName := filepath.Base(os.Args[0])
	// trim extension from app name
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	homePath, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	config := filepath.Join(homePath, ".config", appName, "config.yaml")
	_ = os.MkdirAll(filepath.Dir(config), os.ModePerm)
	if _, err := os.Stat(config); os.IsNotExist(err) {
		configData := flagSet.generateDefaultConfig()
		return ioutil.WriteFile(config, configData, os.ModePerm)
	}
	flagSet.MergeConfigFile(config) // try to read default config after parsing flags
	return nil
}

// generateDefaultConfig generates a default YAML config file for a flagset.
func (flagSet *FlagSet) generateDefaultConfig() []byte {
	hashes := make(map[string]struct{})
	configBuffer := &bytes.Buffer{}
	configBuffer.WriteString("# ")
	configBuffer.WriteString(path.Base(os.Args[0]))
	configBuffer.WriteString(" config file\n# generated by https://github.com/projectdiscovery/goflags\n\n")

	// Attempts to marshal natively if proper flag is set, in case of errors fallback to normal mechanism
	if flagSet.Marshal {
		flagsToMarshall := make(map[string]interface{})

		flagSet.flagKeys.forEach(func(key string, data *flagData) {
			flagsToMarshall[key] = data.defaultValue
		})

		flagSetBytes, err := yaml.Marshal(flagsToMarshall)
		if err == nil {
			configBuffer.Write(flagSetBytes)
			return configBuffer.Bytes()
		}
	}

	flagSet.flagKeys.forEach(func(key string, data *flagData) {
		dataHash := data.Hash()
		if _, ok := hashes[dataHash]; ok {
			return
		}
		hashes[dataHash] = struct{}{}

		configBuffer.WriteString("# ")
		configBuffer.WriteString(strings.ToLower(data.usage))
		configBuffer.WriteString("\n")
		configBuffer.WriteString("#")
		configBuffer.WriteString(data.long)
		configBuffer.WriteString(": ")
		if s, ok := data.defaultValue.(string); ok {
			configBuffer.WriteString(s)
		} else if dv, ok := data.defaultValue.(flag.Value); ok {
			configBuffer.WriteString(dv.String())
		}

		configBuffer.WriteString("\n\n")
	})

	return bytes.TrimSuffix(configBuffer.Bytes(), []byte("\n\n"))
}

// readConfigFile reads the config file and returns any flags
// that might have been set by the config file.
//
// Command line flags however always take precedence over config file ones.
func (flagSet *FlagSet) readConfigFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return errors.Wrap(err, "could not open config file")
	}
	defer file.Close()

	data := make(map[string]interface{})
	err = yaml.NewDecoder(file).Decode(&data)
	if err != nil {
		return errors.Wrap(err, "could not unmarshal config file")
	}
	flag.CommandLine.VisitAll(func(fl *flag.Flag) {
		item, ok := data[fl.Name]
		value := fl.Value.String()

		if strings.EqualFold(fl.DefValue, value) && ok {
			switch data := item.(type) {
			case string:
				_ = fl.Value.Set(data)
			case bool:
				_ = fl.Value.Set(strconv.FormatBool(data))
			case int:
				_ = fl.Value.Set(strconv.Itoa(data))
			case []interface{}:
				for _, v := range data {
					vStr, ok := v.(string)
					if ok {
						_ = fl.Value.Set(vStr)
					}
				}
			}
		}
	})
	return nil
}

// VarP adds a Var flag with a shortname and longname
func (flagSet *FlagSet) VarP(field flag.Value, long, short, usage string) {
	flag.Var(field, short, usage)
	flag.Var(field, long, usage)

	flagData := &flagData{
		usage:        usage,
		short:        short,
		long:         long,
		defaultValue: field,
	}
	flagSet.flagKeys.Set(short, flagData)
	flagSet.flagKeys.Set(long, flagData)
}

// Var adds a Var flag with a longname
func (flagSet *FlagSet) Var(field flag.Value, long, usage string) {
	flag.Var(field, long, usage)

	flagData := &flagData{
		usage:        usage,
		long:         long,
		defaultValue: field,
	}
	flagSet.flagKeys.Set(long, flagData)
}

// StringVarEnv adds a string flag with a shortname and longname with a default value read from env variable
// with a default value fallback
func (flagSet *FlagSet) StringVarEnv(field *string, long, short, defaultValue, envName, usage string) {
	if envValue, exists := os.LookupEnv(envName); exists {
		defaultValue = envValue
	}

	flagSet.StringVarP(field, long, short, defaultValue, usage)
}

// StringVarP adds a string flag with a shortname and longname
func (flagSet *FlagSet) StringVarP(field *string, long, short, defaultValue, usage string) {
	flag.StringVar(field, short, defaultValue, usage)
	flag.StringVar(field, long, defaultValue, usage)

	flagData := &flagData{
		usage:        usage,
		short:        short,
		long:         long,
		defaultValue: defaultValue,
	}
	flagSet.flagKeys.Set(short, flagData)
	flagSet.flagKeys.Set(long, flagData)
}

// StringVar adds a string flag with a longname
func (flagSet *FlagSet) StringVar(field *string, long, defaultValue, usage string) {
	flag.StringVar(field, long, defaultValue, usage)

	flagData := &flagData{
		usage:        usage,
		long:         long,
		defaultValue: defaultValue,
	}
	flagSet.flagKeys.Set(long, flagData)
}

// BoolVarP adds a bool flag with a shortname and longname
func (flagSet *FlagSet) BoolVarP(field *bool, long, short string, defaultValue bool, usage string) {
	flag.BoolVar(field, short, defaultValue, usage)
	flag.BoolVar(field, long, defaultValue, usage)

	flagData := &flagData{
		usage:        usage,
		short:        short,
		long:         long,
		defaultValue: strconv.FormatBool(defaultValue),
	}
	flagSet.flagKeys.Set(short, flagData)
	flagSet.flagKeys.Set(long, flagData)
}

// BoolVar adds a bool flag with a longname
func (flagSet *FlagSet) BoolVar(field *bool, long string, defaultValue bool, usage string) {
	flag.BoolVar(field, long, defaultValue, usage)

	flagData := &flagData{
		usage:        usage,
		long:         long,
		defaultValue: strconv.FormatBool(defaultValue),
	}
	flagSet.flagKeys.Set(long, flagData)
}

// IntVarP adds a int flag with a shortname and longname
func (flagSet *FlagSet) IntVarP(field *int, long, short string, defaultValue int, usage string) {
	flag.IntVar(field, short, defaultValue, usage)
	flag.IntVar(field, long, defaultValue, usage)

	flagData := &flagData{
		usage:        usage,
		short:        short,
		long:         long,
		defaultValue: strconv.Itoa(defaultValue),
	}
	flagSet.flagKeys.Set(short, flagData)
	flagSet.flagKeys.Set(long, flagData)
}

// IntVar adds a int flag with a longname
func (flagSet *FlagSet) IntVar(field *int, long string, defaultValue int, usage string) {
	flag.IntVar(field, long, defaultValue, usage)

	flagData := &flagData{
		usage:        usage,
		long:         long,
		defaultValue: strconv.Itoa(defaultValue),
	}
	flagSet.flagKeys.Set(long, flagData)
}

// StringSliceVarP adds a string slice flag with a shortname and longname
func (flagSet *FlagSet) StringSliceVarP(field *StringSlice, long, short string, defaultValue []string, usage string) {
	for _, item := range defaultValue {
		_ = field.Set(item)
	}

	flag.Var(field, short, usage)
	flag.Var(field, long, usage)

	flagData := &flagData{
		usage:        usage,
		short:        short,
		long:         long,
		defaultValue: field.createStringArrayDefaultValue(),
	}
	flagSet.flagKeys.Set(short, flagData)
	flagSet.flagKeys.Set(long, flagData)
}

// StringSliceVar adds a string slice flag with a longname
func (flagSet *FlagSet) StringSliceVar(field *StringSlice, long string, defaultValue []string, usage string) {
	for _, item := range defaultValue {
		_ = field.Set(item)
	}

	flag.Var(field, long, usage)

	flagData := &flagData{
		usage:        usage,
		long:         long,
		defaultValue: field.createStringArrayDefaultValue(),
	}
	flagSet.flagKeys.Set(long, flagData)
}

func (stringSlice *StringSlice) createStringArrayDefaultValue() string {
	defaultBuilder := &strings.Builder{}
	defaultBuilder.WriteString("[")
	for i, k := range *stringSlice {
		defaultBuilder.WriteString("\"")
		defaultBuilder.WriteString(k)
		defaultBuilder.WriteString("\"")
		if i != len(*stringSlice)-1 {
			defaultBuilder.WriteString(", ")
		}
	}
	defaultBuilder.WriteString("]")
	return defaultBuilder.String()
}

func (flagSet *FlagSet) usageFunc() {
	hashes := make(map[string]struct{})

	cliOutput := flag.CommandLine.Output()
	fmt.Fprintf(cliOutput, "%s\n\n", flagSet.description)
	fmt.Fprintf(cliOutput, "Usage:\n  %s [flags]\n\n", os.Args[0])
	fmt.Fprintf(cliOutput, "Flags:\n")

	writer := tabwriter.NewWriter(cliOutput, 0, 0, 1, ' ', 0)

	flagSet.flagKeys.forEach(func(key string, data *flagData) {
		currentFlag := flag.CommandLine.Lookup(key)

		dataHash := data.Hash()
		if _, ok := hashes[dataHash]; ok {
			return // Don't print the value if printed previously
		}
		hashes[dataHash] = struct{}{}

		result := createUsageString(data, currentFlag)
		fmt.Fprint(writer, result, "\n")
	})
	writer.Flush()
}

func isNotBlank(value string) bool {
	return len(strings.TrimSpace(value)) != 0
}

func createUsageString(data *flagData, currentFlag *flag.Flag) string {
	valueType := reflect.TypeOf(currentFlag.Value)

	result := createUsageFlagNames(data)
	result += createUsageTypeAndDescription(currentFlag, valueType)
	result += createUsageDefaultValue(data, currentFlag, valueType)

	return result
}

func createUsageDefaultValue(data *flagData, currentFlag *flag.Flag, valueType reflect.Type) string {
	if !isZeroValue(currentFlag, currentFlag.DefValue) {
		defaultValueTemplate := " (default "
		switch valueType.String() { // ugly hack because "flag.stringValue" is not exported from the parent library
		case "*flag.stringValue":
			defaultValueTemplate += "%q"
		default:
			defaultValueTemplate += "%v"
		}
		defaultValueTemplate += ")"
		return fmt.Sprintf(defaultValueTemplate, data.defaultValue)
	}
	return ""
}

func createUsageTypeAndDescription(currentFlag *flag.Flag, valueType reflect.Type) string {
	var result string

	flagDisplayType, usage := flag.UnquoteUsage(currentFlag)
	if len(flagDisplayType) > 0 {
		if flagDisplayType == "value" { // hardcoded in the goflags library
			switch valueType.Kind() {
			case reflect.Ptr:
				pointerTypeElement := valueType.Elem()
				switch pointerTypeElement.Kind() {
				case reflect.Slice, reflect.Array:
					switch pointerTypeElement.Elem().Kind() {
					case reflect.String:
						flagDisplayType = "string[]"
					default:
						flagDisplayType = "value[]"
					}
				}
			}
		}
		result += " " + flagDisplayType
	}

	result += "\t\t"
	result += strings.ReplaceAll(usage, "\n", "\n"+strings.Repeat(" ", 4)+"\t")
	return result
}

func createUsageFlagNames(data *flagData) string {
	flagNames := strings.Repeat(" ", 2) + "\t"

	var validFlags []string
	addValidParam := func(value string) {
		if isNotBlank(value) {
			validFlags = append(validFlags, fmt.Sprintf("-%s", value))
		}
	}

	addValidParam(data.short)
	addValidParam(data.long)

	if len(validFlags) == 0 {
		panic("CLI arguments cannot be empty.")
	}

	flagNames += strings.Join(validFlags, ", ")
	return flagNames
}

// isZeroValue determines whether the string represents the zero
// value for a flag.
func isZeroValue(f *flag.Flag, value string) bool {
	// Build a zero value of the flag's Value type, and see if the
	// result of calling its String method equals the value passed in.
	// This works unless the Value type is itself an interface type.
	valueType := reflect.TypeOf(f.Value)
	var zeroValue reflect.Value
	if valueType.Kind() == reflect.Ptr {
		zeroValue = reflect.New(valueType.Elem())
	} else {
		zeroValue = reflect.Zero(valueType)
	}
	return value == zeroValue.Interface().(flag.Value).String()
}
