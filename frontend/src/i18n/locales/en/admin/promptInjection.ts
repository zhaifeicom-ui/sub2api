export default {
  promptInjection: {
    title: 'Prompt Injection Management',
    description: 'Inject consistent System, Developer, or User instructions into every model request for selected groups.',
    addRule: 'Add injection rule', editRule: 'Edit injection rule', empty: 'No injection rules yet',
    emptyHint: 'Add a rule to make selected groups carry a consistent instruction automatically.',
    ruleCount: '{count} rules', searchPlaceholder: 'Search by rule, group, or content',
    positionSummary: 'Recommended: place Developer before the first user message', recommended: 'Recommended', missingGroup: 'Group no longer exists',
    fields: { name: 'Rule name', role: 'Message role', position: 'Message position', content: 'Prompt content' },
    columns: { name: 'Rule', groups: 'Groups', role: 'Role', position: 'Position', content: 'Injected content', actions: 'Actions' },
    placeholders: { name: 'e.g. Unified coding policy', content: 'Enter the complete prompt to inject' },
    roles: { system: 'System', developer: 'Developer', user: 'User' },
    roleHints: { system: 'Base system constraint with the highest priority.', developer: 'Developer instruction for behavior, format, and working style.', user: 'Adds context as a user message for the current task.' },
    positions: {
      prepend: { title: 'Start of message list', short: 'Before original messages', description: 'Placed at the very start of the message array for global preconditions.', explain: 'This message is placed before the client’s existing system, developer, and user messages.' },
      before_first_user: { title: 'Before first user message', short: 'Before the user prompt', description: 'Placed before the user prompt; ideal for Developer behavior instructions.', explain: 'This message is inserted before the first user message. For Developer rules, this is the clearest and most stable choice.' },
      after_last_user: { title: 'After last user message', short: 'After the user prompt', description: 'Appended at the end of the current context for supplemental instructions.', explain: 'This message is appended after the last user message. It is better for supplemental context than core Developer constraints.' }
    },
    positionTechnicalHint: 'Position controls message order, not model priority.', positionExplainTitle: 'How will this rule enter the model request?',
    hints: { groups: 'You can select multiple groups; every model in those groups will use this rule.', content: 'Content is sent to the upstream model as-is. Do not include secrets or private data.' },
    actions: { edit: 'Edit', duplicate: 'Duplicate', delete: 'Delete', cancel: 'Cancel', confirm: 'Submit' },
    validation: { name: 'Enter a rule name', groups: 'Select at least one group', content: 'Enter prompt content' },
    confirmDelete: 'Delete rule “{name}”?', copySuffix: 'Copy', loadFailed: 'Failed to load prompt injection configuration', saveFailed: 'Failed to save prompt injection configuration', saved: 'Prompt injection rule saved', deleted: 'Prompt injection rule deleted'
  }
}
