// ---- plugin:demand_clarification_generator_1 ----
// ============================================================
// 插件 demand_clarification_generator_1 (需求澄清问题生成) 的类型定义
// 由 get_plugin_ai_json 自动生成
// ============================================================

export interface DemandClarificationGeneratorOneInput {
  /** 用户提出的原始需求内容 */
  demand: string;
  /** 之前的对话历史记录 */
  history?: string;
}

/**
 * capabilityClient.load('demand_clarification_generator_1').call<DemandClarificationGeneratorOneOutput>('textGenerate', input)
 * 直接返回此类型，无 .data 包装，直接解构使用：
 * const { response, content } = result;
 */
export interface DemandClarificationGeneratorOneOutput {
  /** [object Object] */
  response?: string;
  /** [object Object] */
  content: string;
}
// ---- end:demand_clarification_generator_1 ----

// ---- plugin:mr_summary_generator_2 ----
// ============================================================
// 插件 mr_summary_generator_2 (MR提交摘要生成) 的类型定义
// 由 get_plugin_ai_json 自动生成
// ============================================================

export interface MrSummaryGeneratorTwoInput {
  /** 代码差异内容 */
  diff: string;
  /** 代码修改说明 */
  changeDesc: string;
}

/**
 * capabilityClient.load('mr_summary_generator_2').call<MrSummaryGeneratorTwoOutput>('textGenerate', input)
 * 直接返回此类型，无 .data 包装，直接解构使用：
 * const { content, response } = result;
 */
export interface MrSummaryGeneratorTwoOutput {
  /** [object Object] */
  content: string;
  /** [object Object] */
  response?: string;
}
// ---- end:mr_summary_generator_2 ----