<script lang="ts">
  type AppInfo = {
    auth_mode: string;
    data_dir: string;
    db_path: string;
  };

  type ExportFormat = {
    id: string;
    label: string;
    description: string;
    status: string;
  };

  let appInfo: AppInfo | null = null;
  let formats: ExportFormat[] = [];
  let error = "";

  async function load() {
    try {
      const [appRes, exportRes] = await Promise.all([
        fetch("/api/v1/app"),
        fetch("/api/v1/export/formats"),
      ]);

      appInfo = await appRes.json();
      const payload = await exportRes.json();
      formats = payload.items ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  load();
</script>

<svelte:head>
  <title>somascope</title>
</svelte:head>

<main class="page">
  <section class="hero">
    <div class="intro panel">
      <p class="eyebrow">Somascope</p>
      <h1>Daily-first wearable data, kept local.</h1>
      <p class="lede">
        V1 is shaped around a local Go server, a Svelte frontend, single-user storage,
        and source-aware exports.
      </p>

      <div class="facts">
        <article>
          <strong>Auth Mode</strong>
          <span>{appInfo?.auth_mode ?? "Loading..."}</span>
        </article>
        <article>
          <strong>Store</strong>
          <span>{appInfo?.db_path ?? "Reserved local SQLite path"}</span>
        </article>
        <article>
          <strong>Scope</strong>
          <span>Fitbit + Oura, daily summary first</span>
        </article>
      </div>
    </div>

    <div class="panel">
      <p class="eyebrow">Exports</p>
      <div class="formats">
        {#each formats as format}
          <article class="format">
            <strong>{format.label}</strong>
            <span>{format.description}</span>
          </article>
        {/each}
      </div>
      {#if error}
        <p class="error">{error}</p>
      {/if}
    </div>
  </section>
</main>

<style>
  .page {
    max-width: 1100px;
    margin: 0 auto;
    padding: 40px 22px 80px;
  }

  .hero {
    display: grid;
    grid-template-columns: 1.35fr 0.95fr;
    gap: 18px;
  }

  .panel {
    border: 1px solid var(--line);
    border-radius: 20px;
    padding: 20px;
    background: var(--panel);
    backdrop-filter: blur(12px);
    box-shadow: 0 12px 32px rgba(24, 32, 25, 0.06);
  }

  .eyebrow {
    margin: 0 0 10px;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.16em;
    font-size: 12px;
  }

  h1 {
    margin: 0;
    max-width: 11ch;
    font-size: clamp(3rem, 8vw, 5rem);
    line-height: 0.92;
  }

  .lede {
    margin-top: 14px;
    color: var(--muted);
    max-width: 46rem;
    line-height: 1.55;
  }

  .facts,
  .formats {
    display: grid;
    gap: 12px;
    margin-top: 18px;
  }

  .facts {
    grid-template-columns: repeat(3, 1fr);
  }

  article {
    border: 1px solid var(--line);
    border-radius: 14px;
    background: rgba(255, 255, 255, 0.45);
    padding: 14px;
  }

  strong {
    display: block;
    margin-bottom: 8px;
    font-size: 13px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  span {
    color: var(--muted);
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  .error {
    color: #8b2d1f;
  }

  @media (max-width: 900px) {
    .hero,
    .facts {
      grid-template-columns: 1fr;
    }
  }
</style>
