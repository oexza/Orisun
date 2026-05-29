import Link from '@docusaurus/Link';
import Layout from '@theme/Layout';
import clsx from 'clsx';
import styles from './index.module.css';

const features = [
  {
    title: 'Command Context Consistency',
    description:
      'Commands declare the event subset they depend on with JSON criteria, then save only if that context is still current.',
  },
  {
    title: 'Durable publishing',
    description:
      'Publisher checkpoints live in PostgreSQL or SQLite, so missed wake-up signals cannot skip committed events.',
  },
  {
    title: 'One deployable server',
    description:
      'Event storage, indexed reads, auth, Admin, gRPC, embedded NATS JetStream, telemetry, and backend-specific release artifacts ship together.',
  },
];

const docs = [
  ['Run a server', '/docs/getting-started', 'Choose SQLite or PostgreSQL and verify the gRPC API.'],
  ['Model consistency', '/docs/concepts/command-context-consistency', 'Scope optimistic concurrency with event-content queries.'],
  ['Write integrations', '/docs/api/eventstore', 'Save, query, subscribe, and manage indexes over gRPC.'],
  ['Operate in production', '/docs/operations/configuration', 'Configure storage, auth, TLS, NATS, telemetry, and clustering.'],
  ['Embed in Go', '/docs/embedding/go', 'Run PostgreSQL-only or SQLite-only Orisun inside your service process.'],
  ['Build and release', '/docs/project/development', 'Work with tests, Docker images, clients, and release notes.'],
];

export default function Home(): JSX.Element {
  return (
    <Layout
      title="Event Store With Embedded Delivery"
      description="Orisun is a batteries-included event store with PostgreSQL or SQLite storage, command context consistency, embedded JetStream delivery, and gRPC APIs."
    >
      <header className={styles.hero}>
        <div className={styles.heroBackdrop} />
        <div className={clsx('container', styles.heroInner)}>
          <div className={styles.heroCopy}>
            <div className={styles.badges}>
              <span className={styles.badge}>v0.2.10</span>
              <span className={styles.badge}>PostgreSQL + SQLite</span>
              <span className={styles.badge}>gRPC + JetStream</span>
            </div>
            <h1>Event storage and live delivery in one server.</h1>
            <p>
              Orisun stores events transactionally, checks consistency by querying event content,
              and delivers catch-up plus live updates through embedded NATS JetStream.
            </p>
            <div className={styles.actions}>
              <Link className="button button--primary button--lg" to="/docs/getting-started">
                Start with Docker
              </Link>
              <Link className="button button--secondary button--lg" to="/docs/api/eventstore">
                Read the API
              </Link>
              <Link className="button button--outline button--lg" href="https://hub.docker.com/r/orexza/orisun">
                Docker Hub
              </Link>
            </div>
          </div>
          <div className={styles.terminal} aria-label="Orisun startup output">
            <div className={styles.stats}>
              <div>
                <span>Consistency</span>
                <strong>CCC</strong>
                <small>JSON criteria checks</small>
              </div>
              <div>
                <span>Delivery</span>
                <strong>0</strong>
                <small>Skipped committed events</small>
              </div>
            </div>
            <pre>{`$ docker run orexza/orisun:sqlite
> gRPC listening on :5005
> publisher checkpoint restored
> events draining sequentially`}</pre>
          </div>
        </div>
      </header>

      <main>
        <section className="section">
          <div className="container">
            <div className={styles.sectionHeader}>
              <span className={styles.eyebrow}>Why Orisun</span>
              <h2>The event log, delivery loop, and consistency boundary live together.</h2>
              <p>
                PostgreSQL or SQLite is the durable source of truth. Embedded JetStream is the
                delivery layer. Your application keeps business decisions in application code.
              </p>
            </div>
            <div className={styles.featureGrid}>
              {features.map((feature) => (
                <article className={styles.featureCard} key={feature.title}>
                  <h3>{feature.title}</h3>
                  <p>{feature.description}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section className={clsx('section', styles.docsSection)}>
          <div className="container">
            <div className={styles.sectionHeader}>
              <span className={styles.eyebrow}>Use Orisun</span>
              <h2>Start with the deployment shape you need.</h2>
              <p>
                Run it as a standalone event store, embed it directly in a Go service, or operate a
                PostgreSQL-backed cluster with the same EventStore API.
              </p>
            </div>
            <div className={styles.docsGrid}>
              {docs.map(([title, href, description]) => (
                <Link className={styles.docCard} to={href} key={href}>
                  <h3>{title}</h3>
                  <p>{description}</p>
                </Link>
              ))}
            </div>
          </div>
        </section>
      </main>
    </Layout>
  );
}
