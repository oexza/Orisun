import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';
import Layout from '@theme/Layout';
import clsx from 'clsx';
import type {ReactNode} from 'react';
import styles from './index.module.css';

type LinkItem = readonly [title: string, href: string, description: string];
type FlowStep = readonly [number: string, title: string, description: string];

const heroFacts = [
  ['Storage', 'PostgreSQL or SQLite'],
  ['Consistency', 'JSON-scoped optimistic writes'],
  ['Delivery', 'Catch-up plus live JetStream'],
];

const guarantees = [
  {
    label: 'Consistency',
    title: 'Command Context Consistency',
    description:
      'Commands declare the event subset they depend on with JSON criteria, then save only if that context is still current.',
  },
  {
    label: 'Delivery',
    title: 'No skipped committed events',
    description:
      'Publisher checkpoints live in PostgreSQL or SQLite. Wake-up signals can be missed; committed events still drain sequentially.',
  },
  {
    label: 'Deployment',
    title: 'One deployable server',
    description:
      'Event storage, indexed reads, auth, the gRPC and Admin APIs, embedded NATS JetStream, and telemetry ship in a single binary.',
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
];

const flow: FlowStep[] = [
  ['1', 'Declare context', 'Select the event subset your command depends on with JSON criteria.'],
  ['2', 'Save atomically', 'Commit only if the selected context is still at the expected position.'],
  ['3', 'Publish in order', 'Drain committed events from durable checkpoints into embedded JetStream.'],
  ['4', 'Project safely', 'Catch up from storage, then consume live events with idempotent checkpoints.'],
];

const useCases = [
  {
    title: 'Embedded event store',
    description:
      'Link Orisun into a Go service when event storage should live inside the same process and deployment unit.',
  },
  {
    title: 'Standalone event service',
    description:
      'Run Orisun as a small gRPC service when multiple applications or languages need the same event API.',
  },
  {
    title: 'Reliable projectors',
    description:
      'Build read models from catch-up subscriptions that recover from downtime without depending only on broker retention.',
  },
];

const docGroups: {title: string; links: LinkItem[]}[] = [
  {
    title: 'Start',
    links: [
      ['Getting Started', '/docs/getting-started', 'Run SQLite or PostgreSQL as a binary, container, or embedded store.'],
      ['Tutorial', '/docs/tutorial', 'Build a ledger with CCC, indexes, and a live projector.'],
    ],
  },
  {
    title: 'Understand',
    links: [
      ['Consistency', '/docs/concepts/command-context-consistency', 'Model business invariants over event-content queries.'],
      ['Delivery guarantees', '/docs/concepts/delivery-guarantees', 'See how checkpoints preserve ordered publishing.'],
      ['Storage backends', '/docs/concepts/storage-backends', 'Choose the right backend for your deployment shape.'],
    ],
  },
  {
    title: 'Build',
    links: [
      ['EventStore API', '/docs/api/eventstore', 'Save, query, subscribe, and manage indexes over gRPC.'],
      ['Go embedding', '/docs/embedding/go', 'Run Orisun inside a Go service with one backend package.'],
      ['Operations', '/docs/operations/configuration', 'Configure auth, TLS, storage, NATS, and telemetry.'],
    ],
  },
];

export default function Home(): ReactNode {
  const diagramUrl = useBaseUrl('/img/orisun-flow.svg');

  return (
    <Layout
      title="Event Store With Embedded Delivery"
      description="Orisun is a batteries-included event store with PostgreSQL or SQLite storage, command context consistency, embedded JetStream delivery, and gRPC APIs."
    >
      <header className={styles.hero}>
        <div className={styles.heroGrid} />
        <div className={clsx('container', styles.heroInner)}>
          <div className={styles.heroCopy}>
            <div className={styles.badges}>
              <span className={styles.badge}>v0.2.10</span>
              <span className={styles.badge}>PostgreSQL + SQLite</span>
              <span className={styles.badge}>gRPC + JetStream</span>
            </div>
            <h1>Event storage and live delivery, built as one system.</h1>
            <p>
              Orisun stores events transactionally, checks consistency by querying event content,
              and delivers catch-up plus live updates through embedded NATS JetStream without
              asking teams to assemble a broker, publisher, and event log by hand.
            </p>
            <div className={styles.actions}>
              <Link className="button button--primary button--lg" to="/docs/getting-started">
                Start locally
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
        <section className={styles.signalBand} aria-label="Core Orisun guarantees">
          <div className={clsx('container', styles.signalGrid)}>
            <div>
              <strong>Per-boundary order</strong>
              <span>Events publish in ascending log position.</span>
            </div>
            <div>
              <strong>Durable checkpoints</strong>
              <span>Publisher state is stored with the selected backend.</span>
            </div>
            <div>
              <strong>Same APIs</strong>
              <span>SQLite and PostgreSQL expose the same gRPC surface.</span>
            </div>
          </div>
        </section>

        <section className="section">
          <div className="container">
            <div className={styles.sectionHeader}>
              <span className={styles.eyebrow}>Why Orisun</span>
              <h2>The event log, consistency boundary, and delivery loop live together.</h2>
              <p>
                PostgreSQL or SQLite is the durable source of truth. Embedded JetStream is the
                delivery layer. Your application keeps business decisions in application code.
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

        <section className={clsx('section', styles.fitSection)}>
          <div className="container">
            <div className={styles.splitHeader}>
              <div>
                <span className={styles.eyebrow}>Where it fits</span>
                <h2>Use Orisun when correctness depends on the event log, not just message delivery.</h2>
              </div>
              <p>
                Orisun is for systems where commands need to read event history, make a business
                decision, commit exactly the events for that decision, and publish the committed log
                in order.
              </p>
            </div>
            <div className={styles.useCaseGrid}>
              {useCases.map((item) => (
                <article className={styles.useCaseCard} key={item.title}>
                  <h3>{item.title}</h3>
                  <p>{item.description}</p>
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
              <h2>From decision to projection, the ordering contract stays explicit.</h2>
              <p>
                Orisun keeps storage, consistency checks, publishing, and subscriber recovery on
                one ordered path so application code can focus on domain decisions.
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
