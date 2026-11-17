import { ChangeEvent, FormEvent, useEffect, useState } from "react";

type Step = { label: string; state: string };

type Job = {
  id: string;
  title: string;
  address: string;
  status: string;
  error: string;
  runId: string;
  filename: string;
  checklist: Step[];
};

type Account = { id: string; email: string };

type StepView = "done" | "current" | "stopped" | "waiting";

const stepText: Record<StepView, string> = {
  done: "Done",
  current: "In progress",
  stopped: "Stopped",
  waiting: "Waiting",
};

const icons = {
  house: (
    <>
      <path d="M3 10.5 12 3l9 7.5" />
      <path d="M5 9.5V20h14V9.5" />
      <path d="M9.5 20v-5.5h5V20" />
    </>
  ),
  pin: (
    <>
      <path d="M12 21s-7-6.2-7-11.5a7 7 0 0 1 14 0C19 14.8 12 21 12 21z" />
      <circle cx="12" cy="9.5" r="2.5" />
    </>
  ),
  doc: (
    <>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5" />
      <path d="M9 13h6" />
      <path d="M9 17h4" />
    </>
  ),
  hash: (
    <>
      <path d="M5 9h14" />
      <path d="M5 15h14" />
      <path d="M10 4 8 20" />
      <path d="m16 4-2 16" />
    </>
  ),
  check: <path d="M5 12.5 10 17.5 19 7.5" />,
  alert: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7.5v5.5" />
      <path d="M12 16.5h.01" />
    </>
  ),
  play: <path d="M7 4.5v15l13-7.5z" fill="currentColor" />,
};

class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { credentials: "include", ...init });
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const message = data && typeof data.error === "string" ? data.error : res.statusText;
    throw new ApiError(message, res.status);
  }
  return data as T;
}

function stepView(job: Job, index: number): StepView {
  if (job.checklist[index].state === "done") {
    return "done";
  }
  if (index !== job.checklist.findIndex((step) => step.state === "pending")) {
    return "waiting";
  }
  if (job.status === "running") {
    return "current";
  }
  return job.status === "failed" ? "stopped" : "waiting";
}

export function App() {
  const [me, setMe] = useState<Account | null>(null);
  const [ready, setReady] = useState(false);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [selected, setSelected] = useState<Job | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api<Account>("/api/me")
      .then(setMe)
      .catch(() => setMe(null))
      .finally(() => setReady(true));
  }, []);

  useEffect(() => {
    if (!me) {
      setJobs([]);
      return;
    }
    api<Job[]>("/api/jobs")
      .then(setJobs)
      .catch((err: Error) => setError(err.message));
  }, [me]);

  const runningId = jobs.find((job) => job.status === "running")?.id;

  useEffect(() => {
    if (!runningId) {
      return;
    }
    let stopped = false;
    let timer = 0;
    const poll = () => {
      api<Job>("/api/jobs/" + runningId)
        .then((job) => {
          if (!stopped) {
            showJob(job);
          }
        })
        .catch((err: unknown) => {
          if (err instanceof ApiError && err.status === 401) {
            setMe(null);
          }
        })
        .finally(() => {
          if (!stopped) {
            timer = window.setTimeout(poll, 1000);
          }
        });
    };
    timer = window.setTimeout(poll, 1000);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, [runningId]);

  function showJob(job: Job) {
    setSelected((current) => (current?.id === job.id ? job : current));
    setJobs((current) => current.map((item) => (item.id === job.id ? job : item)));
  }

  if (!ready) {
    return <p className="loading muted">Loading…</p>;
  }
  if (!me) {
    return <Auth onIn={setMe} />;
  }

  return (
    <>
      <header className="topbar">
        <div className="topbar-inner">
          <Brand />
          <div className="account">
            <span className="muted">{me.email}</span>
            <button
              onClick={() => {
                api("/api/logout", { method: "POST" }).finally(() => {
                  setMe(null);
                  setSelected(null);
                });
              }}
            >
              Log out
            </button>
          </div>
        </div>
      </header>
      <main className="board">
        {error ? (
          <p className="alert" role="alert">
            <Icon name="alert" />
            {error}
          </p>
        ) : null}
        <div className="side">
          <Create
            onCreated={(job) => {
              setJobs((current) => [job, ...current]);
              setSelected(job);
              setError("");
            }}
            onError={setError}
          />
          <section className="card sheets">
            <div className="card-head">
              <h2>Sheets</h2>
              <span className="muted small">{jobs.length === 1 ? "1 sheet" : jobs.length + " sheets"}</span>
            </div>
            <ul className="list">
              {jobs.length === 0 ? <li className="muted">No sheets yet.</li> : null}
              {jobs.map((job) => (
                <li key={job.id}>
                  <button
                    className={selected?.id === job.id ? "row on" : "row"}
                    aria-current={selected?.id === job.id ? "true" : undefined}
                    onClick={() => setSelected(job)}
                  >
                    <span className="row-top">
                      <span className="row-text">
                        <strong>{job.title}</strong>
                        <span className="muted">{job.address}</span>
                      </span>
                      <span className={"chip " + job.status}>{job.status}</span>
                    </span>
                    <span className={"progress " + job.status} aria-hidden="true">
                      {job.checklist.map((step, index) => (
                        <span key={step.label} className={stepView(job, index)} />
                      ))}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        </div>
        {selected ? (
          <Sheet job={selected} onChange={showJob} onError={setError} />
        ) : (
          <p className="card empty muted">Select a sheet to see its checklist.</p>
        )}
      </main>
    </>
  );
}

function Icon({ name, size = 18 }: { name: keyof typeof icons; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {icons[name]}
    </svg>
  );
}

function Brand() {
  return (
    <div className="brand">
      <span className="brand-mark">
        <Icon name="house" size={20} />
      </span>
      <div className="brand-text">
        <span className="eyebrow">Real estate</span>
        <h1>Listing board</h1>
      </div>
    </div>
  );
}

function Auth({ onIn }: { onIn: (account: Account) => void }) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const account = await api<Account>("/api/" + mode, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      onIn(account);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not sign in");
    }
  }

  function switchTo(next: "login" | "register") {
    setMode(next);
    setError("");
  }

  return (
    <main className="auth">
      <div className="auth-box">
        <Brand />
        <form className="card auth-form" onSubmit={submit}>
          <div className="segmented" role="group" aria-label="Account">
            <button type="button" aria-pressed={mode === "login"} onClick={() => switchTo("login")}>
              Sign in
            </button>
            <button type="button" aria-pressed={mode === "register"} onClick={() => switchTo("register")}>
              Create account
            </button>
          </div>
          <label>
            Email
            <input
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              type="email"
              autoComplete="email"
              required
            />
          </label>
          <label>
            Password
            <input
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              type="password"
              autoComplete={mode === "login" ? "current-password" : "new-password"}
              required
            />
          </label>
          {error ? (
            <p className="form-error" role="alert">
              <Icon name="alert" size={16} />
              {error}
            </p>
          ) : null}
          <button type="submit" className="primary">
            {mode === "login" ? "Sign in" : "Create account"}
          </button>
        </form>
      </div>
    </main>
  );
}

function Create({ onCreated, onError }: { onCreated: (job: Job) => void; onError: (message: string) => void }) {
  const [title, setTitle] = useState("");
  const [address, setAddress] = useState("");
  const [file, setFile] = useState<File | null>(null);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    if (!file) {
      onError("Choose a brochure file");
      return;
    }
    let job: Job;
    try {
      job = await api<Job>("/api/jobs", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title, address }),
      });
    } catch (err) {
      onError(err instanceof Error ? err.message : "Could not create the sheet");
      return;
    }
    form.reset();
    setTitle("");
    setAddress("");
    setFile(null);
    const body = new FormData();
    body.append("file", file);
    try {
      onCreated(await api<Job>("/api/jobs/" + job.id + "/file", { method: "POST", body }));
    } catch (err) {
      onCreated(job);
      onError(err instanceof Error ? err.message : "Could not attach the brochure");
    }
  }

  return (
    <form className="card create" onSubmit={submit}>
      <h2>New sheet</h2>
      <label>
        Title
        <input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Market loft" required />
      </label>
      <label>
        Address
        <input
          value={address}
          onChange={(event) => setAddress(event.target.value)}
          placeholder="10 Market Street"
          required
        />
      </label>
      <label>
        Brochure
        <span className="drop">
          <input type="file" onChange={(event) => setFile(event.target.files?.[0] ?? null)} required />
          <span className="muted small">PDF or plain text, up to 1 MiB</span>
        </span>
      </label>
      <button type="submit" className="primary">
        Create sheet
      </button>
    </form>
  );
}

function Sheet({
  job,
  onChange,
  onError,
}: {
  job: Job;
  onChange: (job: Job) => void;
  onError: (message: string) => void;
}) {
  async function start() {
    onError("");
    try {
      const next = await api<Job>("/api/jobs/" + job.id + "/start", { method: "POST" });
      onChange(next);
    } catch (err) {
      onError(err instanceof Error ? err.message : "Could not start the sheet");
    }
  }

  async function attach(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }
    onError("");
    const body = new FormData();
    body.append("file", file);
    try {
      const next = await api<Job>("/api/jobs/" + job.id + "/file", { method: "POST", body });
      onChange(next);
    } catch (err) {
      onError(err instanceof Error ? err.message : "Could not attach the brochure");
    }
  }

  const done = job.checklist.filter((step) => step.state === "done").length;

  return (
    <article className="card sheet">
      <div className="sheet-head">
        <div className="sheet-title">
          <span className="eyebrow">Listing sheet</span>
          <h2>{job.title}</h2>
          <span className="address">
            <Icon name="pin" size={16} />
            {job.address}
          </span>
        </div>
        {job.status === "success" || job.status === "failed" ? (
          <span className={"stamp " + job.status}>{job.status === "success" ? "Published" : "Failed"}</span>
        ) : (
          <span className={"chip " + job.status}>
            {job.status === "running" ? <span className="pulse" aria-hidden="true" /> : null}
            {job.status}
          </span>
        )}
      </div>
      {job.error ? (
        <p className="alert" role="alert">
          <Icon name="alert" />
          {job.error}
        </p>
      ) : null}
      {job.filename ? (
        <div className="tiles">
          <div className="tile">
            <span className="tile-icon">
              <Icon name="doc" />
            </span>
            <span className="tile-text">
              <span className="muted">Brochure</span>
              <strong>{job.filename}</strong>
            </span>
            {job.status !== "failed" ? (
              <a href={"/api/jobs/" + job.id + "/file"} download>
                Download
              </a>
            ) : null}
          </div>
          {job.runId ? (
            <div className="tile">
              <span className="tile-icon">
                <Icon name="hash" />
              </span>
              <span className="tile-text">
                <span className="muted">Run</span>
                <code title={job.runId}>{job.runId.slice(0, 8)}</code>
              </span>
            </div>
          ) : null}
        </div>
      ) : job.status === "draft" ? (
        <label className="drop">
          Attach a brochure
          <input type="file" onChange={attach} />
          <span className="muted small">PDF or plain text, up to 1 MiB</span>
        </label>
      ) : null}
      <section className="checklist" aria-live="polite">
        <div className="section-head">
          <h3>Checklist</h3>
          <span className="muted small">
            {done} of {job.checklist.length} steps done
          </span>
        </div>
        <ol className="steps">
          {job.checklist.map((step, index) => {
            const view = stepView(job, index);
            const dot = {
              done: <Icon name="check" size={16} />,
              current: <span className="pulse" />,
              stopped: "!",
              waiting: index + 1,
            }[view];
            return (
              <li key={step.label} className={"step " + view}>
                <span className="step-track" aria-hidden="true">
                  <span className="dot">{dot}</span>
                  {index < job.checklist.length - 1 ? <span className="line" /> : null}
                </span>
                <span className="step-label">{step.label}</span>
                <span className="step-state">{stepText[view]}</span>
              </li>
            );
          })}
        </ol>
      </section>
      {job.status === "draft" ? (
        <div className="actions">
          <button className="primary" onClick={start} disabled={!job.filename}>
            <Icon name="play" size={14} />
            Start
          </button>
          {job.filename ? null : <span className="muted">Attach a brochure first</span>}
        </div>
      ) : null}
    </article>
  );
}
