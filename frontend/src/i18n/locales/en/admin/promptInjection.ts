export default {
  promptInjection: {
    title: 'Prompt Injection Management',
    description: 'Inject managed instructions into OpenAI Chat Completions, Responses, and WebSocket requests by group or upstream account.',
    warning: 'Injected content is sent to the upstream model and can affect every matching request. Test with a narrow account scope first.',
    addRule: 'Add rule', editRule: 'Edit rule', empty: 'No injection rules yet. Add one to get started.',
    unsaved: 'Unsaved changes', synced: 'Configuration synced', saveAll: 'Save all changes',
    enabledCount: '{enabled} enabled / {total} total',
    fields: { name: 'Rule name', enabled: 'Enabled', scope: 'Scope', target: 'Target', role: 'Message role', position: 'Position', models: 'Models', priority: 'Priority', content: 'Prompt content' },
    placeholders: { name: 'e.g. Support response policy', target: 'Select a target', models: 'Blank means all models; comma-separated, e.g. gpt-5*, gpt-4.1', content: 'Enter the complete prompt to inject' },
    scopes: { group: 'Group', account: 'Account' }, roles: { system: 'System', developer: 'Developer', user: 'User' },
    positions: { prepend: 'Start of message list', before_first_user: 'Before first user message', after_last_user: 'After last user message' },
    hints: { priority: 'Higher numbers apply first; group rules precede account rules at equal priority.', models: 'Supports case-insensitive * wildcards.', role: 'Codex OAuth promotes eligible System / Developer content into instructions.' },
    actions: { edit: 'Edit', duplicate: 'Duplicate', delete: 'Delete', cancel: 'Cancel', confirm: 'Confirm' },
    validation: { name: 'Enter a rule name', target: 'Select a target', content: 'Enter prompt content' },
    loadFailed: 'Failed to load prompt injection configuration', saveFailed: 'Failed to save prompt injection configuration', saved: 'Prompt injection configuration saved',
    targetsFailed: 'Failed to load groups or accounts', unnamed: 'Unnamed target', allModels: 'All models'
  }
}
