import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docs: [
    'overview',
    'comparison',
    'start-here',
    'getting-started',
    'tutorial',
    {
      type: 'category',
      label: 'Concepts',
      collapsed: false,
      items: [
        'concepts/command-context-consistency',
        'concepts/positions',
        'concepts/storage-backends',
        'concepts/delivery-guarantees',
        'concepts/indexing',
      ],
    },
    {
      type: 'category',
      label: 'Patterns',
      collapsed: false,
      items: [
        'patterns/idempotency-and-retry',
        'patterns/projection-rebuild',
        'patterns/event-scopes',
        'patterns/event-schema-evolution',
      ],
    },
    {
      type: 'category',
      label: 'API',
      collapsed: false,
      items: ['api/eventstore', 'api/admin', 'api/clients'],
    },
    {
      type: 'category',
      label: 'Operations',
      collapsed: false,
      items: [
        'operations/configuration',
        'operations/security',
        'operations/deployment',
        'operations/observability',
        'operations/troubleshooting',
      ],
    },
    {
      type: 'category',
      label: 'Embedding',
      collapsed: false,
      items: ['embedding/go'],
    },
    'internals',
    {
      type: 'category',
      label: 'Project',
      collapsed: false,
      items: ['project/development', 'project/releases'],
    },
  ],
};

export default sidebars;
