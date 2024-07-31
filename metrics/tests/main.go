package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	promTestUnit "github.com/red-hat-storage/ocs-operator/metrics/v4/tests/prometheus/testunit"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	k8sYAML "k8s.io/apimachinery/pkg/util/yaml"
)

const templateExtention = ".template"

type AlertLogger struct {
	logOut *log.Logger
	logErr *log.Logger
}

func NewAlertLogger(verbose bool) *AlertLogger {
	var outWriter io.Writer = os.Stdout
	outPrefix := "[INFO]  "
	errPrefix := "[ERROR] "
	if !verbose {
		outWriter = io.Discard
	}
	aLogger := &AlertLogger{
		logOut: log.New(outWriter, outPrefix, log.LstdFlags),
		logErr: log.New(os.Stderr, errPrefix, log.LstdFlags),
	}
	return aLogger
}

func (l *AlertLogger) Errorf(err error, msg string, v ...any) {
	if err != nil {
		msg = msg + " :Error: %v"
		v = append(v, err)
		l.logErr.Printf(msg, v...)
	}
}

func (l *AlertLogger) ExitOnErrorf(err error, msg string, v ...any) {
	l.Errorf(err, msg, v...)
	if err != nil {
		os.Exit(1)
	}
}

func (l *AlertLogger) Infof(msg string, v ...any) {
	l.logOut.Printf(msg, v...)
}

func (l *AlertLogger) NBlankLines(n uint) {
	lFlag := l.logOut.Flags()
	lPrefix := l.logOut.Prefix()
	l.logOut.SetFlags(0)
	l.logOut.SetPrefix("")
	for i := uint(0); i < n; i++ {
		l.logOut.Println()
	}
	l.logOut.SetFlags(lFlag)
	l.logOut.SetPrefix(lPrefix)
}

type commaSeparatedVars struct {
	vars []string
}

func (v *commaSeparatedVars) String() (ret string) {
	if len(v.vars) == 0 {
		return
	}
	return strings.Join(v.vars, ", ")
}
func (v *commaSeparatedVars) Set(setStr string) (err error) {
	setStr = strings.ReplaceAll(setStr, ",", " ")
	v.vars = strings.Fields(setStr)
	return
}

var _ flag.Value = &commaSeparatedVars{}

type FlagVars struct {
	verbose       bool
	metricsDir    string
	outputDir     string
	ruleFiles     *commaSeparatedVars
	testTemplates *commaSeparatedVars
}

func NewFlagVars() *FlagVars {
	fVars := FlagVars{ruleFiles: &commaSeparatedVars{}, testTemplates: &commaSeparatedVars{}}
	alertTestFlagSet := flag.NewFlagSet("test-alerts", flag.ExitOnError)
	alertTestFlagSet.BoolVar(&fVars.verbose, "verbose", false, "turn on the verbose mode")
	alertTestFlagSet.StringVar(&fVars.metricsDir, "metrics-dir", "", "path to the metrics dir location.\nThis argument is necessary, if user is not explicitly providing 'rule-files' and 'test-templates' args.")
	alertTestFlagSet.StringVar(&fVars.outputDir, "output-dir", "tmp", "output dir location")
	alertTestFlagSet.Var(fVars.ruleFiles, "rule-files",
		"comma separated prometheus rule files in jsonnet/json/yaml format (default \"prometheus-ocs-rules.yaml\" file in 'metrics/deploy' directory)")
	alertTestFlagSet.Var(fVars.testTemplates, "test-templates", "comma separated template file/directory names.\nIf a directory name is provided, only '.template' extention files will be picked from the directory")
	var _ = alertTestFlagSet.Parse(os.Args[1:])
	return &fVars
}

// ExecCommand function allows to run any arbitrary command
// 'pipedToCmds' commands, if provided, will process the output from previous commands
//
// Eg: ls -ltrh |grep "test" |wc -l
//
// can be run as follows,
//
// ExecCommand("ls -ltrh", "grep \"test\"", "wc -l")
//
// the function returns the output of the last command
// on any error during command execution, it will return a non-nil error value
func ExecCommand(cmdLine string, pipedToCmds ...string) (*bytes.Buffer, error) {
	allPipedCmds := make([]*exec.Cmd, len(pipedToCmds)+1)
	var allStrCmds = []string{cmdLine}
	allStrCmds = append(allStrCmds, pipedToCmds...)
	var cmd *exec.Cmd
	ch := make(chan struct{})
	var err error
	for indx := range allPipedCmds {
		cmdArgs := strings.Fields(allStrCmds[indx])
		allPipedCmds[indx] = exec.Command(cmdArgs[0], cmdArgs[1:]...)
		allPipedCmds[indx].Stdout, allPipedCmds[indx].Stderr = os.Stdout, os.Stderr
		if indx > 0 {
			r, w := io.Pipe()
			allPipedCmds[indx-1].Stdout = w
			allPipedCmds[indx].Stdin = r
			allPipedCmds[indx].Stdout = os.Stdout
			err = allPipedCmds[indx-1].Start()
			if err != nil {
				break
			}
			go func() {
				ch <- struct{}{}
				defer w.Close()
				_ = allPipedCmds[indx-1].Wait()
			}()
			// make sure go routine has started
			<-ch
		}
		cmd = allPipedCmds[indx]
	}
	// check for any previous command errors
	if err != nil {
		return nil, err
	}
	buf := &bytes.Buffer{}
	cmd.Stdout = buf
	err = cmd.Start()
	if err != nil {
		return nil, err
	}
	return buf, cmd.Wait()
}

// ProcessJSONAlertFile process all the json/yaml and jsonnet files.
//
// It's main function is to receive 'PrometheusRule' file (in any of the above format)
// and create corresponding 'spec.group' files. From the test template file passed, it will create
// the actual test json file. To this created test json file, we add the group files as headers.
//
// The function will exit immediately on any error.
func ProcessJSONAlertFile(jsonFiles, testTemplates []string, outputDirPath string, l *AlertLogger) {
	// preliminary checks
	if l == nil {
		l = NewAlertLogger(true)
	}
	//handle json files
	var existingJSONFilesMap = make(map[string]struct{})
	for _, jsonFileName := range jsonFiles {
		fInfo, err := os.Stat(jsonFileName)
		if err != nil {
			l.Errorf(err, "failed to access json file: %q", jsonFileName)
			continue
		} else if fInfo.IsDir() {
			l.Errorf(errors.New("found a directory instead of a file"),
				"expecting a json file: %q", jsonFileName)
			continue
		}
		existingJSONFilesMap[jsonFileName] = struct{}{}
	}
	if len(existingJSONFilesMap) == 0 {
		l.ExitOnErrorf(errors.New("no file provided"),
			"no prometheus-rule file found to process, nothing to test")
	}

	// handle template files
	allTemplateFiles := listAllTemplateFiles(testTemplates, l)
	for indx, eachTemplateFile := range allTemplateFiles {
		allTemplateFiles[indx] = prepareTemplateFile(eachTemplateFile, outputDirPath, l)
	}
	if len(allTemplateFiles) == 0 {
		l.ExitOnErrorf(errors.New("no test template file(s) provided"), "no valid test template found in: %+v", testTemplates)
	}

	// now extract the 'spec.groups' from prometheus rule files
	// this is needed for 'promtool' program
	var groupFileBaseNames string
	for jsonFileName := range existingJSONFilesMap {
		l.Infof("Processing file: %q", jsonFileName)
		// now create a file containing only the PrometheusRule spec
		// this is necessary as the 'promtool' only recognise the spec file
		var rule monitoringv1.PrometheusRule
		jsonBytes, err := os.ReadFile(jsonFileName)
		l.ExitOnErrorf(err, "failed to read file: %q", jsonFileName)
		// below is an extra attempt to extract the spec.groups field
		var decodeInto any = &rule
		for i := 0; i < 2; i++ {
			// here we try to decode a complete 'PrometheusRule' object,
			err = k8sYAML.NewYAMLOrJSONDecoder(bytes.NewBuffer(jsonBytes), 1000).Decode(decodeInto)
			l.ExitOnErrorf(err, "failed to unmarshal file: %q", jsonFileName)
			if len(rule.Spec.Groups) == 0 {
				// in some cases, we get, file containing direct 'spec -> groups' part of PrometheusRule
				// so on a second attempt we try to decode only the Spec part
				decodeInto = &rule.Spec
			} else {
				break
			}
		}
		// if groups are still empty, then it is an invalid file
		// log the error and continue with the next file
		if len(rule.Spec.Groups) == 0 {
			l.Errorf(errors.New("empty 'groups' found"),
				"no valid 'groups' item found in file: %q", jsonFileName)
			continue
		}
		jsonBytes, err = json.MarshalIndent(rule.Spec, "", "    ")
		l.ExitOnErrorf(err, "failed to marshall the json to bytes")

		var groupsFileName = fmt.Sprintf("%s.groups", filepath.Join(outputDirPath, filepath.Base(jsonFileName)))
		groupsJSONFile, err := os.Create(groupsFileName)
		l.ExitOnErrorf(err, "failed to create the spec.groups file: %q", groupsFileName)
		_, err = groupsJSONFile.Write(jsonBytes)
		// error or not, close the created groups file
		groupsJSONFile.Close()
		l.ExitOnErrorf(err, "failed to write the groups only json file")
		l.Infof("Created PrometheusRule spec groups file: %q\n", groupsFileName)
		groupFileBaseNames = groupFileBaseNames + fmt.Sprintf("%q ", filepath.Base(groupsFileName))
	}
	l.NBlankLines(1)

	groupFileBaseNames = strings.TrimSpace(groupFileBaseNames)
	groupFileBaseNames = strings.ReplaceAll(groupFileBaseNames, " ", ", ")
	// if we were unable to find any valid groups,
	// exit with an error message
	if groupFileBaseNames == "" {
		l.ExitOnErrorf(errors.New("empty 'groups' file list"),
			"could not extract any valid 'groups' file from JSON file list: %+v", jsonFiles)
	}

	replaceRegex := regexp.MustCompile(`\.template$`)
	var allJSONTestFilesMap = make(map[string]struct{})
	// here we are processing the template files which contain the tests
	for _, templateFile := range allTemplateFiles {
		// use the template file to generate a proper alert test file
		templateFileBaseFile := filepath.Base(templateFile)
		t := template.Must(template.ParseFiles(templateFile))
		outputTestFile := replaceRegex.ReplaceAllString(templateFileBaseFile, "")
		outputTestFile = filepath.Join(outputDirPath, outputTestFile)
		// if the file is already present, continue
		if _, ok := allJSONTestFilesMap[outputTestFile]; ok {
			l.Infof("Duplicate entry found for output file: %q", outputTestFile)
			continue
		}
		testAlertFile, err := os.Create(outputTestFile)
		l.ExitOnErrorf(err, "failed to create alert test file: %q", outputTestFile)
		// execute the template and create the alert test json file
		err = t.Execute(testAlertFile,
			struct{ PrometheusRuleFile string }{PrometheusRuleFile: groupFileBaseNames})
		// error or not, close the alert test file
		testAlertFile.Close()
		l.ExitOnErrorf(err, "failed to execute the template: %+v", t)
		// collect the test file names as map-keys, so that there is no duplicates
		allJSONTestFilesMap[outputTestFile] = struct{}{}
		l.Infof("Created alert test file: %s", outputTestFile)
	}

	var anyPromToolErr error
	for testF := range allJSONTestFilesMap {
		l.NBlankLines(1)
		promtoolCmdStr := fmt.Sprintf("promtool test rules %s", testF)
		l.Infof("Running 'promtool' command: %q", promtoolCmdStr)
		b, err := ExecCommand(promtoolCmdStr)
		l.Errorf(err, "failed to run 'promtool' command for file: %q", testF)
		if err != nil {
			anyPromToolErr = err
			continue
		}
		bStr := strings.TrimSpace(b.String())
		l.Infof("Promtool output:\n%s", bStr)
	}
	l.ExitOnErrorf(anyPromToolErr, "promtool error")
}

func listAllTemplateFiles(templates []string, l *AlertLogger) (existingTemplates []string) {
	for _, t := range templates {
		fInfo, err := os.Stat(t)
		if err != nil {
			l.Errorf(err, "unable to get info on file: %q", t)
			continue
		}
		if fInfo.IsDir() {
			dirEntries, err := os.ReadDir(t)
			if err != nil {
				l.Errorf(err, "unable to list files in the directory: %q", t)
				continue
			}
			for _, f := range dirEntries {
				fName := filepath.Join(t, f.Name())
				if !f.IsDir() && strings.HasSuffix(fName, templateExtention) {
					existingTemplates = append(existingTemplates, fName)
				}
			}
		} else {
			existingTemplates = append(existingTemplates, t)
		}
	}
	return
}

// prepareTemplateFile function will try to add '{{.PrometheusRuleFile}}'
// template line to the provided template file.
// On a successful completion, a new template file will be written to the
// output directory and the function will return this new file path.
// On any error it will return the given/provided file name, without any modification.
func prepareTemplateFile(templateFileName, outputDir string, l *AlertLogger) string {
	// EXPERIMENTAL CODE
	var ptUnit promTestUnit.PrometheusTestUnit
	jsonBytes, err := os.ReadFile(templateFileName)
	if err != nil {
		return templateFileName
	}
	err = k8sYAML.NewYAMLOrJSONDecoder(bytes.NewBuffer(jsonBytes), 1000).Decode(&ptUnit)
	if err != nil {
		l.Errorf(err, "error while converting %q file to json/yaml format", templateFileName)
		return templateFileName
	}
	// check whether 'RuleFiles' already have a '{{.PrometheusRuleFile}}'
	haveTemplateLine := false
	for _, ruleFile := range ptUnit.RuleFiles {
		if strings.Contains(ruleFile, "{{.PrometheusRuleFile}}") {
			haveTemplateLine = true
			break
		}
	}
	if !haveTemplateLine {
		ptUnit.RuleFiles = append(ptUnit.RuleFiles, "{{.PrometheusRuleFile}}")
	}
	templateBaseName := filepath.Base(templateFileName)
	newTemplateFileName := filepath.Join(outputDir, templateBaseName)
	if !strings.HasSuffix(newTemplateFileName, templateExtention) {
		newTemplateFileName = fmt.Sprintf("%s%s", newTemplateFileName, templateExtention)
	}
	jsonBytes, err = json.MarshalIndent(ptUnit, "", "    ")
	if err != nil {
		return templateFileName
	}
	newTemplateFile, err := os.Create(newTemplateFileName)
	if err != nil {
		return templateFileName
	}
	splitLines := strings.Split(string(jsonBytes), fmt.Sprintln())
	for _, line := range splitLines {
		if strings.Contains(line, "{{.PrometheusRuleFile}}") {
			line = strings.Replace(line, `"{{.PrometheusRuleFile}}"`, `{{.PrometheusRuleFile}}`, -1)
		}
		_, err = newTemplateFile.WriteString(fmt.Sprintln(line))
		if err != nil {
			break
		}
	}
	newTemplateFile.Close()
	if err != nil {
		return templateFileName
	}
	return newTemplateFileName
	// EXPERIMENTAL CODE ENDS
}

func processJsonnetFiles(jFiles []string, outputDir string, l *AlertLogger) []string {
	var allJSONFiles []string
	var allJsonnetFiles []string
	for _, jFile := range jFiles {
		if strings.HasSuffix(jFile, ".jsonnet") {
			allJsonnetFiles = append(allJsonnetFiles, jFile)
		} else {
			allJSONFiles = append(allJSONFiles, jFile)
		}
	}
	for _, jsonnetFile := range allJsonnetFiles {
		jsonnetCmdStr := fmt.Sprintf("jsonnet -m %s %s", outputDir, jsonnetFile)
		l.Infof("Running 'jsonnet' command: %q", jsonnetCmdStr)
		b, err := ExecCommand(jsonnetCmdStr)
		if err != nil {
			l.Errorf(err,
				"error while running 'jsonnet' command, please check the command is available in your system and the passed file: %q is appropriate", jsonnetFile)
			continue
		}
		l.NBlankLines(1)
		// output will be each json file paths separated by new line
		bStr := strings.TrimSpace(b.String())
		var jsonFiles []string
		if jsonFiles = strings.Split(bStr, fmt.Sprintln()); len(jsonFiles) == 0 || jsonFiles[0] == "" {
			l.Errorf(errors.New("no output found"), "empty 'jsonnet' command output")
			continue
		}
		allJSONFiles = append(allJSONFiles, jsonFiles...)
	}
	return allJSONFiles
}

func processOutputDir(outputDir string) error {
	fInfo, err := os.Stat(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(outputDir, 0755)
		}
	} else if !fInfo.IsDir() {
		err = fmt.Errorf("not a directory, but an existing file")
	}
	return err
}

func checkPrerequisites(fVars *FlagVars) error {
	requiredCommands := []string{"promtool"}
	if fVars != nil && fVars.ruleFiles != nil {
		for _, ruleFile := range fVars.ruleFiles.vars {
			if strings.HasSuffix(ruleFile, ".jsonnet") {
				requiredCommands = append(requiredCommands, "jsonnet")
				break
			}
		}
	}
	for _, reqCmd := range requiredCommands {
		if _, execErr := exec.LookPath(reqCmd); execErr != nil {
			return execErr
		}
	}
	return nil
}

func main() {
	fVars := NewFlagVars()
	l := NewAlertLogger(fVars.verbose)
	if fVars.metricsDir == "" &&
		(len(fVars.testTemplates.vars) == 0 || len(fVars.ruleFiles.vars) == 0) {
		l.ExitOnErrorf(errors.New("empty metrics dir path found"), "Either provide a non-empty value to 'metrics-dir' arg (to fetch default tests) or give valid values to '-rule-files' and 'test-templates' arguments")
	}
	l.ExitOnErrorf(checkPrerequisites(fVars), "Needed prerequisite checks failed")

	if len(fVars.testTemplates.vars) == 0 {
		_ = fVars.testTemplates.Set(filepath.Join(fVars.metricsDir, "tests", "templates"))
	}
	if len(fVars.ruleFiles.vars) == 0 {
		_ = fVars.ruleFiles.Set(filepath.Join(fVars.metricsDir, "deploy", "prometheus-ocs-rules.yaml"))
	}

	l.Infof("Metrics directory : %s", fVars.metricsDir)
	l.Infof("Rule files        : %s", fVars.ruleFiles)
	l.Infof("Template files    : %s", fVars.testTemplates.String())
	l.Infof("Output directory  : %s", fVars.outputDir)
	l.NBlankLines(1)

	// before we start, let's make sure output dir is valid and it exists
	l.ExitOnErrorf(processOutputDir(fVars.outputDir),
		"unable to handle output directory: %q", fVars.outputDir)
	jsonFiles := processJsonnetFiles(fVars.ruleFiles.vars, fVars.outputDir, l)
	if len(jsonFiles) == 0 {
		l.ExitOnErrorf(errors.New("no files to process"), "empty json files received")
	}
	ProcessJSONAlertFile(jsonFiles, fVars.testTemplates.vars, fVars.outputDir, l)
	l.NBlankLines(1)
}
