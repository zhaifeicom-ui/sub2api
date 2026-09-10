export default {
  promptInjection: {
    title: '提示词注入管理',
    description: '按分组或上游账号，把管理提示词注入 OpenAI Chat Completions、Responses 与 WebSocket 请求。',
    warning: '注入内容会发送给上游模型，并可能影响全部命中请求。建议先用小范围账号验证。',
    addRule: '新增规则',
    editRule: '编辑规则',
    empty: '暂无注入规则，点击“新增规则”开始配置。',
    unsaved: '有未保存的更改',
    synced: '配置已同步',
    saveAll: '保存全部更改',
    enabledCount: '已启用 {enabled} / 共 {total} 条',
    fields: {
      name: '规则名称', enabled: '启用', scope: '作用范围', target: '目标', role: '消息角色',
      position: '注入位置', models: '适用模型', priority: '优先级', content: '提示词内容'
    },
    placeholders: {
      name: '例如：客服回答规范', target: '请选择目标', models: '留空表示全部模型；多个模型用逗号分隔，如 gpt-5*, gpt-4.1',
      content: '输入需要注入的完整提示词'
    },
    scopes: { group: '分组', account: '账号' },
    roles: { system: 'System', developer: 'Developer', user: 'User' },
    positions: { prepend: '消息列表最前', before_first_user: '第一条用户消息前', after_last_user: '最后一条用户消息后' },
    hints: {
      priority: '数字越大越先应用；同优先级时分组规则先于账号规则。',
      models: '支持 * 通配符，匹配不区分大小写。',
      role: 'Codex OAuth 会把可提升的 System / Developer 内容合并到 instructions。'
    },
    actions: { edit: '编辑', duplicate: '复制', delete: '删除', cancel: '取消', confirm: '确定' },
    validation: { name: '请输入规则名称', target: '请选择目标', content: '请输入提示词内容' },
    loadFailed: '加载提示词注入配置失败', saveFailed: '保存提示词注入配置失败', saved: '提示词注入配置已保存',
    targetsFailed: '加载分组或账号列表失败', unnamed: '未命名目标', allModels: '全部模型'
  }
}
