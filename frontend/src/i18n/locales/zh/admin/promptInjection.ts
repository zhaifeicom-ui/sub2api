export default {
  promptInjection: {
    title: '提示词注入管理',
    description: '把统一的 System、Developer 或 User 指令注入到指定分组的全部模型请求中。',
    addRule: '新增注入规则',
    editRule: '编辑注入规则',
    empty: '暂无注入规则',
    emptyHint: '新增一条规则，即可让指定分组的请求自动携带统一指令。',
    ruleCount: '共 {count} 条规则',
    searchPlaceholder: '搜索规则名称、分组或内容',
    positionSummary: '推荐：Developer 放在第一条用户消息之前',
    recommended: '推荐',
    missingGroup: '分组已不存在',
    fields: { name: '规则名称', role: '消息角色', position: '注入位置', content: '提示词内容' },
    columns: { name: '规则', groups: '生效分组', role: '角色', position: '消息位置', content: '注入内容', actions: '操作' },
    placeholders: { name: '例如：统一代码规范', content: '输入需要注入的完整提示词' },
    roles: { system: 'System', developer: 'Developer', user: 'User' },
    roleHints: {
      system: '基础系统约束，优先级最高。',
      developer: '开发者指令，适合定义行为、格式和工作方式。',
      user: '以用户消息形式加入上下文，适合补充任务背景。'
    },
    positions: {
      prepend: {
        title: '消息列表最前', short: '在原始消息之前',
        description: '放在请求消息数组的最前面，适合全局前置策略。',
        explain: '这条消息会先于客户端原有的 system、developer、user 消息进入请求。适合需要最早建立上下文的规则。'
      },
      before_first_user: {
        title: '第一条用户消息前', short: '在用户问题之前',
        description: '位于用户问题之前，适合 Developer 行为指令。',
        explain: '这条消息会插入到第一条 user 消息之前。对于 Developer 规则，这是最直观也最稳定的选择：先给模型工作规范，再处理用户问题。'
      },
      after_last_user: {
        title: '最后一条用户消息后', short: '在用户问题之后',
        description: '放在当前对话末尾，适合补充本轮上下文。',
        explain: '这条消息会放在最后一条 user 消息之后。它更像对当前输入的追加说明，不建议把核心 Developer 约束放在这里。'
      }
    },
    positionTechnicalHint: '位置决定消息数组中的顺序，不是模型优先级。',
    positionExplainTitle: '这条规则会怎样进入模型请求？',
    hints: {
      groups: '可同时选择多个分组；这些分组下的所有模型都会应用本规则。',
      content: '内容会原样发送给上游模型，请避免放入密钥、隐私或不应暴露的信息。'
    },
    actions: { edit: '编辑', duplicate: '复制', delete: '删除', cancel: '取消', confirm: '提交' },
    validation: { name: '请输入规则名称', groups: '至少选择一个分组', content: '请输入提示词内容' },
    confirmDelete: '确定删除规则“{name}”吗？',
    copySuffix: '副本',
    loadFailed: '加载提示词注入配置失败', saveFailed: '保存提示词注入配置失败', saved: '提示词注入规则已保存', deleted: '提示词注入规则已删除'
  }
}
