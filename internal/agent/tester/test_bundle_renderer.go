package tester

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"devflow/internal/agent/common"
)

type moduleContract struct {
	SchemaVersion   int      `json:"schema_version"`
	Kind            string   `json:"kind"`
	ModuleID        string   `json:"module_id"`
	ModuleName      string   `json:"module_name"`
	DeliveryProfile string   `json:"delivery_profile"`
	EntryFiles      []string `json:"entry_files"`
	PublicAPI       []struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	} `json:"public_api"`
}

func parseModuleContract(content string) (moduleContract, error) {
	var contract moduleContract
	if err := json.Unmarshal([]byte(content), &contract); err != nil {
		return moduleContract{}, fmt.Errorf("parse module contract json: %w", err)
	}
	if contract.SchemaVersion != 1 {
		return moduleContract{}, fmt.Errorf("module contract schema_version must be 1")
	}
	if strings.TrimSpace(contract.Kind) != "module_contract" {
		return moduleContract{}, fmt.Errorf("module contract kind must be module_contract")
	}
	if strings.TrimSpace(contract.ModuleID) == "" {
		return moduleContract{}, fmt.Errorf("module contract module_id is required")
	}
	if len(contract.PublicAPI) == 0 {
		return moduleContract{}, fmt.Errorf("module contract public_api is required")
	}
	for i := range contract.PublicAPI {
		contract.PublicAPI[i].Name = strings.TrimSpace(contract.PublicAPI[i].Name)
		contract.PublicAPI[i].Kind = strings.TrimSpace(contract.PublicAPI[i].Kind)
		if contract.PublicAPI[i].Name == "" {
			return moduleContract{}, fmt.Errorf("module contract public_api[%d].name is required", i)
		}
	}
	for i := range contract.EntryFiles {
		contract.EntryFiles[i] = strings.TrimSpace(contract.EntryFiles[i])
	}
	if !strings.EqualFold(strings.TrimSpace(contract.DeliveryProfile), "frontend_web") {
		if len(contract.EntryFiles) == 0 || contract.EntryFiles[0] == "" {
			return moduleContract{}, fmt.Errorf("module contract entry_files[0] is required")
		}
	}
	return contract, nil
}

func buildBaseFullTestBundle(_ testDataInputs, contract moduleContract, seedBundle common.TestFileBundle) (common.TestFileBundle, error) {
	moduleID := strings.TrimSpace(contract.ModuleID)
	if moduleID == "" {
		return common.TestFileBundle{}, fmt.Errorf("module contract module_id is required")
	}
	if strings.TrimSpace(seedBundle.ModuleID) != moduleID {
		return common.TestFileBundle{}, fmt.Errorf("seed_tests module_id %q does not match module_contract module_id %q", seedBundle.ModuleID, moduleID)
	}

	testPath := "test/" + moduleID + ".full.test.js"
	bundle := common.TestFileBundle{
		SchemaVersion: 1,
		Kind:          "full_test_files",
		ModuleID:      moduleID,
		TestCommand:   "node " + testPath,
		TestFiles: []common.TestFile{
			{
				Path:    testPath,
				Content: buildBaseFullTestContent(contract),
			},
		},
	}
	return bundle, nil
}

func renderFullTestBundleWithCases(base common.TestFileBundle, contract moduleContract, cases []TestCase) common.TestFileBundle {
	rendered := base
	if len(rendered.TestFiles) == 0 {
		return rendered
	}
	rendered.TestFiles = append([]common.TestFile(nil), base.TestFiles...)
	rendered.TestFiles[0] = common.TestFile{
		Path:    base.TestFiles[0].Path,
		Content: renderFullTestContent(base.TestFiles[0].Content, contract, cases),
	}
	return rendered
}

func buildBaseFullTestContent(contract moduleContract) string {
	if strings.EqualFold(strings.TrimSpace(contract.DeliveryProfile), "frontend_web") {
		return `const assert = require('assert');
const fs = require('fs');

assert.ok(fs.existsSync('index.html'));
assert.ok(fs.existsSync('README.md'));
assert.ok(fs.existsSync('src'));
assert.ok(fs.statSync('src').isDirectory());
assert.match(fs.readFileSync('index.html', 'utf8'), /script|module|src\//i);

console.log('frontend_web full tests passed');
`
	}

	entryFile := contract.EntryFiles[0]
	var builder strings.Builder
	builder.WriteString("const assert = require('assert');\n")
	builder.WriteString("const api = require(")
	builder.WriteString(strconv.Quote("../" + entryFile))
	builder.WriteString(");\n\n")
	for _, api := range contract.PublicAPI {
		builder.WriteString("assert.ok(Object.prototype.hasOwnProperty.call(api, ")
		builder.WriteString(strconv.Quote(api.Name))
		builder.WriteString("), ")
		builder.WriteString(strconv.Quote(api.Name + " must be exported by module_contract public_api"))
		builder.WriteString(");\n")
		if api.Kind == "function" {
			builder.WriteString("assert.strictEqual(typeof api[")
			builder.WriteString(strconv.Quote(api.Name))
			builder.WriteString("], 'function', ")
			builder.WriteString(strconv.Quote(api.Name + " must be a function"))
			builder.WriteString(");\n")
		}
	}
	builder.WriteString("\nconsole.log(")
	builder.WriteString(strconv.Quote(contract.ModuleID + " full tests passed"))
	builder.WriteString(");\n")
	return builder.String()
}

func renderFullTestContent(baseContent string, contract moduleContract, cases []TestCase) string {
	if len(cases) == 0 {
		return baseContent
	}

	trimmed := strings.TrimRight(baseContent, "\n")
	var builder strings.Builder
	builder.WriteString(trimmed)
	builder.WriteString("\n\n")

	if strings.EqualFold(strings.TrimSpace(contract.DeliveryProfile), "frontend_web") {
		for _, testCase := range cases {
			builder.WriteString("// case ")
			builder.WriteString(testCase.Name)
			builder.WriteString(" [")
			builder.WriteString(testCase.Type)
			builder.WriteString("] target=")
			builder.WriteString(testCase.Target)
			builder.WriteString("\n")
			builder.WriteString("// scenario: ")
			builder.WriteString(testCase.Scenario)
			builder.WriteString("\n")
			builder.WriteString("// expected: ")
			builder.WriteString(testCase.Expected)
			builder.WriteString("\n")
			builder.WriteString("assert.match(fs.readFileSync('index.html', 'utf8'), /script|module|src\\//i, ")
			builder.WriteString(strconv.Quote(testCase.Name + ": index.html should keep app entry invariant"))
			builder.WriteString(");\n\n")
		}
		builder.WriteString("console.log('frontend_web full tests passed with plan cases');\n")
		return builder.String()
	}

	for _, testCase := range cases {
		builder.WriteString("// case ")
		builder.WriteString(testCase.Name)
		builder.WriteString(" [")
		builder.WriteString(testCase.Type)
		builder.WriteString("] target=")
		builder.WriteString(testCase.Target)
		builder.WriteString("\n")
		builder.WriteString("// scenario: ")
		builder.WriteString(testCase.Scenario)
		builder.WriteString("\n")
		builder.WriteString("// expected: ")
		builder.WriteString(testCase.Expected)
		builder.WriteString("\n")
		builder.WriteString("assert.ok(Object.prototype.hasOwnProperty.call(api, ")
		builder.WriteString(strconv.Quote(testCase.Target))
		builder.WriteString("), ")
		builder.WriteString(strconv.Quote(testCase.Name + ": target must remain exported"))
		builder.WriteString(");\n")
		if contractPublicAPIKind(contract, testCase.Target) == "function" {
			builder.WriteString("assert.strictEqual(typeof api[")
			builder.WriteString(strconv.Quote(testCase.Target))
			builder.WriteString("], 'function', ")
			builder.WriteString(strconv.Quote(testCase.Name + ": target must remain callable"))
			builder.WriteString(");\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("console.log(")
	builder.WriteString(strconv.Quote(contract.ModuleID + " full tests passed with plan cases"))
	builder.WriteString(");\n")
	return builder.String()
}

func contractPublicAPIKind(contract moduleContract, target string) string {
	for _, api := range contract.PublicAPI {
		if api.Name == target {
			return api.Kind
		}
	}
	return ""
}
