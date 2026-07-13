import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';
import Layout from '@theme/Layout';
import clsx from 'clsx';
import type {ReactNode} from 'react';
import styles from './index.module.css';

type LinkItem = readonly [title: string, href: string, description: string];
type FlowStep = readonly [number: string, title: string, description: string];

const heroFacts = [
  ['Storage', 'PostgreSQL, YugabyteDB, SQLite, or FDB beta'],
  ['Decisions', 'Context-validated writes'],
  ['Delivery', 'Catch-up plus live JetStream'],
];

const guarantees = [
  {
    label: 'Decide',
    title: 'Commit only against current context',
    description:
      'A command declares the event subset its decision depends on with JSON criteria. Orisun saves only if that declared context is still current. CCC is the native model; DCB-style append conditions use the same mechanism.',
  },
  {
    label: 'Delivery',
    title: 'No skipped committed events',
    description:
      'Durable publisher checkpoints drain committed events in ascending log position per boundary, even when wake-up signals are missed.',
  },
  {
    label: 'Deployment',
    title: 'One deployable server',
    description:
      'Event storage, indexed reads, auth, the gRPC and Admin APIs, embedded NATS JetStream, and telemetry ship together in each backend-specific binary.',
  },
  {
    label: 'Portability',
    title: 'Same API on every backend',
    description:
      'SQLite, PostgreSQL, YugabyteDB, and FoundationDB expose the identical gRPC surface, so deployments grow without client changes.',
  },
];

const backends = [
  {
    name: 'SQLite',
    href: '/docs/getting-started#run-sqlite-from-a-binary',
    summary: 'Single-node production, embedded apps, local development, and low-ops services.',
    details: ['event log', 'admin state', 'indexes', 'publisher checkpoints'],
  },
  {
    name: 'PostgreSQL',
    href: '/docs/getting-started#run-postgresql-from-a-binary',
    summary: 'Multi-node Orisun deployments backed by database-managed storage and operational tooling.',
    details: ['cluster locks', 'schemas', 'PgBouncer-friendly config', 'shared storage'],
  },
  {
    name: 'YugabyteDB',
    href: '/docs/getting-started#run-yugabytedb-from-a-binary',
    summary: 'PostgreSQL-compatible distributed SQL with Orisun committed-watermark reads.',
    details: ['YSQL', 'pg_notify wake-ups', 'advisory locks', 'committed watermark'],
  },
  {
    name: 'FoundationDB',
    href: '/docs/operations/foundationdb',
    summary: 'Beta clustered backend for ordered key-value deployments and parallel commits.',
    details: ['FDB leases', 'versionstamps', 'covering indexes', 'Linux artifacts'],
  },
];

const flow: FlowStep[] = [
  ['1', 'Read context', 'Select the event history your command depends on with JSON criteria.'],
  ['2', 'Validate the decision', 'Reject the write if that context moved; otherwise commit atomically.'],
  ['3', 'Publish in order', 'Drain committed events per boundary from durable checkpoints into embedded JetStream.'],
  ['4', 'Project safely', 'Catch up from storage, then consume live events with idempotent checkpoints.'],
];

const docGroups: {title: string; links: LinkItem[]}[] = [
  {
    title: 'Start',
    links: [
      ['What is Orisun?', '/docs', 'The guarantees Orisun makes and the shortest path to each task.'],
      ['Getting Started', '/docs/getting-started', 'Run SQLite or PostgreSQL as a binary, container, or embedded store.'],
      ['Tutorial', '/docs/tutorial', 'Build a ledger with CCC, indexes, and a live projector.'],
    ],
  },
  {
    title: 'Understand',
    links: [
      ['Comparing Orisun', '/docs/comparison', 'How Orisun differs from Kafka, EventStoreDB, Postgres LISTEN/NOTIFY, and NATS JetStream.'],
      ['Consistency', '/docs/concepts/command-context-consistency', 'Model business invariants over event-content queries.'],
      ['Dynamic Consistency Boundaries', '/docs/concepts/dynamic-consistency-boundaries', 'Map DCB event types, tags, and append conditions to Orisun APIs.'],
      ['Delivery guarantees', '/docs/concepts/delivery-guarantees', 'See how checkpoints preserve ordered publishing.'],
      ['Storage backends', '/docs/concepts/storage-backends', 'Choose the right backend for your deployment shape.'],
    ],
  },
  {
    title: 'Build',
    links: [
      ['EventStore API', '/docs/api/eventstore', 'Save, query, subscribe, and manage indexes over gRPC.'],
      ['Go embedding', '/docs/embedding/go', 'Run Orisun inside a Go service with one backend package.'],
      ['Operations', '/docs/operations/deployment', 'Deploy binaries or containers, then configure auth, TLS, storage, and telemetry.'],
    ],
  },
];

export default function Home(): ReactNode {
  const diagramUrl = useBaseUrl('/img/orisun-flow.svg');

  return (
    <Layout
      title="The Event Database for Decisions That Must Stay Correct"
      description="Orisun preserves complete event history, validates each declared command context at commit time, and publishes committed events sequentially within each boundary."
    >
      <header className={styles.hero}>
        <div className={styles.heroGrid} />
        <div className={clsx('container', styles.heroInner)}>
          <div className={styles.heroCopy}>
            <div className={styles.badges}>
              <a
                className={styles.releaseBadge}
                href="https://github.com/oexza/Orisun/releases/latest"
                aria-label="Latest Orisun release"
              >
                <img
                  src="https://img.shields.io/github/v/release/oexza/Orisun?label=release"
                  alt="Latest release"
                />
              </a>
              <span className={styles.badge}>Context-validated writes</span>
              <span className={styles.badge}>gRPC + JetStream</span>
            </div>
            <h1>The event database for decisions that stay correct.</h1>
            <p>
              State-based databases show what is true now, but not how it became true. Orisun
              preserves the complete event history and lets each command declare exactly which
              events its decision depends on. If that context changes, the write is rejected;
              otherwise its new events commit atomically and publish sequentially within the
              boundary. The same gRPC API runs on SQLite, PostgreSQL, YugabyteDB, or FoundationDB.
            </p>
            <div className={styles.actions}>
              <Link className="button button--primary button--lg" to="/docs/getting-started">
                Get started
              </Link>
              <Link className="button button--secondary button--lg" to="/docs/api/eventstore">
                Read the API
              </Link>
              <Link className="button button--outline button--lg" href="https://hub.docker.com/r/orexza/orisun">
                Docker Hub
              </Link>
            </div>
            <dl className={styles.heroFacts}>
              {heroFacts.map(([label, value]) => (
                <div key={label}>
                  <dt>{label}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
          </div>
          <aside className={styles.heroPanel} aria-label="Orisun architecture overview">
            <div className={styles.diagramFrame}>
              <img src={diagramUrl} alt="Orisun command, storage, and JetStream delivery flow" />
            </div>
            <div className={styles.heroRoute}>
              <div>
                <span>Recommended first read</span>
                <strong>What is Orisun?</strong>
              </div>
              <Link to="/docs">Read</Link>
            </div>
            <div className={styles.terminal}>
              <div className={styles.terminalTop}>
                <span />
                <span />
                <span />
              </div>
              <pre>{`$ ./orisun-sqlite
> gRPC listening on :5005
> publisher checkpoint restored
> events draining sequentially`}</pre>
            </div>
          </aside>
        </div>
      </header>

      <main>
        <section className="section">
          <div className="container">
            <div className={styles.sectionHeader}>
              <span className={styles.eyebrow}>Why Orisun</span>
              <h2>Store the facts. Validate the decision. Deliver the result.</h2>
              <p>
                The selected storage backend is the durable source of truth and embedded
                JetStream is the live delivery layer. Your application owns the business
                decision; Orisun checks its declared context before committing the resulting events.
              </p>
            </div>
            <div className={styles.featureGrid}>
              {guarantees.map((feature) => (
                <article className={styles.featureCard} key={feature.title}>
                  <span>{feature.label}</span>
                  <h3>{feature.title}</h3>
                  <p>{feature.description}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section className={clsx('section', styles.backendSection)}>
          <div className="container">
            <div className={styles.sectionHeader}>
              <span className={styles.eyebrow}>Backend choices</span>
              <h2>Use the smallest operational shape that fits.</h2>
              <p>
                SQLite is complete for single-node systems. PostgreSQL adds multi-node Orisun
                coordination and database-managed operations without changing client code.
              </p>
            </div>
            <div className={styles.backendGrid}>
              {backends.map((backend) => (
                <Link className={styles.backendCard} to={backend.href} key={backend.name}>
                  <div className={styles.backendHeader}>
                    <h3>{backend.name}</h3>
                    <span>Open guide</span>
                  </div>
                  <p>{backend.summary}</p>
                  <ul>
                    {backend.details.map((detail) => (
                      <li key={detail}>{detail}</li>
                    ))}
                  </ul>
                </Link>
              ))}
            </div>
          </div>
        </section>

        <section className="section">
          <div className="container">
            <div className={styles.sectionHeader}>
              <span className={styles.eyebrow}>Command flow</span>
              <h2>Each write is checked against the context that made it valid.</h2>
              <p>
                Orisun keeps storage, consistency checks, publishing, and subscriber recovery on
                one ordered path per boundary so application code can focus on domain decisions.
              </p>
            </div>
            <div className={styles.flowGrid}>
              {flow.map(([number, title, description]) => (
                <article className={styles.flowStep} key={title}>
                  <strong>{number}</strong>
                  <h3>{title}</h3>
                  <p>{description}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section className={clsx('section', styles.docsSection)}>
          <div className="container">
            <div className={styles.sectionHeader}>
              <span className={styles.eyebrow}>Documentation</span>
              <h2>Find the right page for the job.</h2>
              <p>
                The docs are organized around the work teams actually do: start a server, model a
                consistency boundary, integrate the API, embed Orisun, and operate it.
              </p>
            </div>
            <div className={styles.docGroups}>
              {docGroups.map((group) => (
                <section className={styles.docGroup} key={group.title}>
                  <h3>{group.title}</h3>
                  {group.links.map(([title, href, description]) => (
                    <Link className={styles.docCard} to={href} key={href}>
                      <span>{title}</span>
                      <p>{description}</p>
                    </Link>
                  ))}
                </section>
              ))}
            </div>
          </div>
        </section>

        <section className={styles.ctaSection}>
          <div className={clsx('container', styles.ctaInner)}>
            <div>
              <span className={styles.eyebrow}>Get moving</span>
              <h2>Start with a binary or container, then keep the same API as you scale.</h2>
            </div>
            <div className={styles.ctaActions}>
              <Link className="button button--primary button--lg" to="/docs/tutorial">
                Follow the tutorial
              </Link>
              <Link className="button button--secondary button--lg" to="/docs/embedding/go">
                Embed in Go
              </Link>
            </div>
          </div>
        </section>
      </main>
    </Layout>
  );
}
