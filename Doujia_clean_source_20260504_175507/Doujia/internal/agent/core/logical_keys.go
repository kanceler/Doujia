package core

import "fmt"

const (
	LKRequirement        = "requirement"
	LKPMPlan             = "pm_plan"
	LKReviewPlanFailure  = "review_plan_failure"
	LKRepairInstruction  = "repair_instruction"
	LKRewriteInstruction = "rewrite_instruction"

	LKArchitecturePlan = "architecture_plan"
	LKEnvironmentSpec  = "environment_spec"
	LKRuntimeContract  = "runtime_contract"
	LKContainerContext = "container_context"

	LKModuleSpecs = "module_specs"

	LKModule01Spec       = "module01_spec"
	LKModule01CoderTask  = "module01_coder_task"
	LKModule01TesterTask = "module01_tester_task"
	LKModule01Contract   = "module01_contract"
	LKModule01SeedTests  = "module01_seed_tests"

	LKModule02Spec       = "module02_spec"
	LKModule02CoderTask  = "module02_coder_task"
	LKModule02TesterTask = "module02_tester_task"
	LKModule02Contract   = "module02_contract"
	LKModule02SeedTests  = "module02_seed_tests"

	LKModuleSpec     = "module_spec"
	LKCoderTask      = "coder_task"
	LKTesterTask     = "tester_task"
	LKModuleContract = "module_contract"
	LKSeedTests      = "seed_tests"

	LKCoderBranch              = "coder_branch"
	LKWriteCodeFailure         = "write_code_failure"
	LKContainerSelfTestFailure = "container_self_test_failure"
	LKUpstreamArtifactIssue    = "upstream_artifact_issue"

	LKExpandedTestData = "expanded_test_data"
	LKBoundaryTests    = "boundary_tests"
	LKFullTestFiles    = "full_test_files"
	LKModuleTestReport = "module_test_report"

	LKModule01CoderBranch      = "module01_coder_branch"
	LKModule01ModuleTestReport = "module01_module_test_report"
	LKModule02CoderBranch      = "module02_coder_branch"
	LKModule02ModuleTestReport = "module02_module_test_report"

	LKMergedCodeSummary    = "merged_code_summary"
	LKMergedMainBranch     = "merged_main_branch"
	LKMergedMainBranchNote = "merged_main_branch_note"
	LKMergeCodeReport      = "merge_code_report"

	LKGlobalTestData        = "global_test_data"
	LKGlobalAcceptanceTests = "global_acceptance_tests"
	LKGlobalTestCommands    = "global_test_commands"
	LKGlobalTestDataFailure = "global_test_data_failure"
	LKGlobalTestReport      = "global_test_report"
	LKDeliveryGuide         = "delivery_guide"

	LKCommandStdout = "command_stdout"
	LKCommandStderr = "command_stderr"

	LKRunDeliveryConfig = "run_delivery_config"
)

func ModuleSpecKey(moduleID string) string {
	return fmt.Sprintf("%s_spec", moduleID)
}

func ModuleCoderTaskKey(moduleID string) string {
	return fmt.Sprintf("%s_coder_task", moduleID)
}

func ModuleTesterTaskKey(moduleID string) string {
	return fmt.Sprintf("%s_tester_task", moduleID)
}

func ModuleContractKey(moduleID string) string {
	return fmt.Sprintf("%s_contract", moduleID)
}

func ModuleSeedTestsKey(moduleID string) string {
	return fmt.Sprintf("%s_seed_tests", moduleID)
}

func ModuleCoderBranchKey(moduleID string) string {
	return fmt.Sprintf("%s_coder_branch", moduleID)
}

func ModuleTestReportKey(moduleID string) string {
	return fmt.Sprintf("%s_module_test_report", moduleID)
}
