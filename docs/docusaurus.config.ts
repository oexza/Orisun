import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Orisun',
  tagline: 'Event storage and live delivery in one server',
  favicon: 'img/orisun-flow.svg',
  url: 'https://oexza.github.io',
  baseUrl: '/Orisun/',
  organizationName: 'oexza',
  projectName: 'Orisun',
  onBrokenLinks: 'throw',
  trailingSlash: false,

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          routeBasePath: '/docs',
          editUrl: 'https://github.com/oexza/Orisun/tree/main/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/orisun-flow.svg',
    metadata: [
      {
        name: 'description',
        content:
          'Orisun is a batteries-included event store with PostgreSQL or SQLite storage, command context consistency, embedded JetStream delivery, and gRPC APIs.',
      },
    ],
    navbar: {
      title: 'Orisun',
      logo: {
        alt: 'Orisun',
        src: 'img/orisun-flow.svg',
      },
      items: [
        {to: '/docs/getting-started', label: 'Docs', position: 'left'},
        {to: '/docs/tutorial', label: 'Tutorial', position: 'left'},
        {to: '/docs/api/eventstore', label: 'API', position: 'left'},
        {to: '/docs/operations/configuration', label: 'Operations', position: 'left'},
        {href: 'https://github.com/oexza/Orisun/releases', label: 'Releases', position: 'right'},
        {href: 'https://hub.docker.com/r/orexza/orisun', label: 'Docker', position: 'right'},
        {href: 'https://github.com/oexza/Orisun', label: 'GitHub', position: 'right'},
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Getting Started', to: '/docs/getting-started'},
            {label: 'Tutorial', to: '/docs/tutorial'},
            {label: 'Concepts', to: '/docs/concepts/command-context-consistency'},
            {label: 'EventStore API', to: '/docs/api/eventstore'},
            {label: 'Embedding', to: '/docs/embedding/go'},
          ],
        },
        {
          title: 'Operations',
          items: [
            {label: 'Configuration', to: '/docs/operations/configuration'},
            {label: 'Deployment', to: '/docs/operations/deployment'},
            {label: 'Troubleshooting', to: '/docs/operations/troubleshooting'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/oexza/Orisun'},
            {label: 'Releases', href: 'https://github.com/oexza/Orisun/releases'},
            {label: 'Docker Hub', href: 'https://hub.docker.com/r/orexza/orisun'},
          ],
        },
      ],
      copyright: `Copyright ${new Date().getFullYear()} Orisun contributors. Released under the MIT License.`,
    },
    prism: {
      additionalLanguages: ['bash', 'go', 'json', 'yaml', 'protobuf'],
    },
    tableOfContents: {
      minHeadingLevel: 2,
      maxHeadingLevel: 3,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
