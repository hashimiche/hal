"use strict";

// The page never sees a token: it talks to the BFF with a session cookie,
// and the trace it shows holds decoded claims only.

const DECISION_POINTS = {
  idp: "the IdP",
  persona: "the persona's own rights",
  ceiling: "the ceiling",
  task_scope: "the task scope",
};
const AUTHORS = { persona: "You", agent: "Demo agent", error: "Error" };

const $ = (id) => document.getElementById(id);
let taskCount = 0;

class LoginRequired extends Error {}

// Builds an element. Strings become text nodes, never HTML.
function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === "class") node.className = value;
    else node.setAttribute(key, value);
  }
  for (const child of children) {
    if (child !== null && child !== undefined) node.append(child instanceof Node ? child : String(child));
  }
  return node;
}

async function api(path, body) {
  const options = body === undefined ? {} : {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
  const response = await fetch(path, options);
  const data = await response.json().catch(() => ({}));
  if (response.status === 401) throw new LoginRequired();
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}

// --- Views ---------------------------------------------------------------

function showLogin(message) {
  $("app").hidden = true;
  $("who").hidden = true;
  $("login").hidden = false;
  if (message) $("login-message").textContent = message;
}

function showApp(persona) {
  $("persona").textContent = persona;
  $("login").hidden = true;
  $("who").hidden = false;
  $("app").hidden = false;
  $("prompt").focus();
}

function say(role, ...content) {
  const item = el("li", { class: `message ${role}` }, el("span", { class: "author" }, AUTHORS[role]), ...content);
  $("messages").append(item);
  item.scrollIntoView({ block: "end" });
  return item;
}

function text(value) {
  return el("p", { class: "text" }, value);
}

function failed(error) {
  if (error instanceof LoginRequired) showLogin("Your login has expired. Log in again.");
  else say("error", text(error.message));
}

function setBusy(busy) {
  for (const control of document.querySelectorAll("#ask input, #ask button, .suggestions button")) {
    control.disabled = busy;
  }
}

// --- Chat ----------------------------------------------------------------

async function ask(prompt) {
  say("persona", text(prompt));
  setBusy(true);
  try {
    const plan = await api("/api/plan", { prompt });
    if (plan.task_id) {
      askConsent(prompt, plan);
      return;
    }
    say("agent", text(plan.answer));
  } catch (error) {
    failed(error);
  }
  setBusy(false);
}

function askConsent(prompt, plan) {
  const allow = el("button", { type: "button" }, "Allow");
  const decline = el("button", { type: "button", class: "secondary" }, "Decline");
  const actions = el("div", { class: "actions" }, allow, decline);
  say("agent",
    text("To answer, I need your consent to this task scope. I will get one token for it, for this task only:"),
    el("ul", { class: "scopes" }, ...plan.scopes.map((scope) => el("li", {}, el("code", {}, scope)))),
    actions);

  allow.addEventListener("click", async () => {
    actions.replaceChildren(el("span", { class: "muted" }, "Allowed. Working…"));
    try {
      const result = await api("/api/run", { task_id: plan.task_id });
      say("agent", text(result.answer));
      showTrace(prompt, result.trace);
    } catch (error) {
      failed(error);
    }
    setBusy(false);
  });
  decline.addEventListener("click", () => {
    actions.replaceChildren(el("span", { class: "muted" }, "Declined."));
    say("agent", text("Understood. I did nothing and read no data."));
    setBusy(false);
  });
}

// --- Transparency panel --------------------------------------------------

function showTrace(prompt, trace) {
  taskCount += 1;
  $("tasks").prepend(el("article", { class: "task" },
    el("h3", {}, `Task ${taskCount}: “${prompt}”`),
    details([["Task scope", codes(trace.task_scope)]]),
    exchangeView(trace.exchange),
    trace.obo ? oboView(trace.obo) : null,
    ...trace.tool_calls.map(toolCallView)));
}

function exchangeView(exchange) {
  return el("section", {},
    el("h4", {}, "IdP"),
    details([
      ["Token exchange", exchange.ok ? badge("allowed", "OBO token issued") : badge("refused", "refused")],
      ["IdP said", exchange.error],
      ["Decided by", exchange.decided_by ? DECISION_POINTS[exchange.decided_by] : null],
    ]));
}

function oboView(obo) {
  return el("section", {},
    el("h4", {}, "OBO token"),
    details([
      ["sub (persona)", el("code", {}, obo.sub ?? "?")],
      ["act.sub (actor)", el("code", {}, obo.act?.sub ?? JSON.stringify(obo.act ?? null))],
      ["aud", el("code", {}, Array.isArray(obo.aud) ? obo.aud.join(", ") : obo.aud ?? "?")],
      ["authorization_details", el("pre", {}, JSON.stringify(obo.authorization_details ?? null, null, 2))],
    ]));
}

function toolCallView(call) {
  const args = Object.values(call.args).join(", ");
  return el("section", { class: "call" },
    el("h4", {}, el("code", {}, `${call.tool}(${args})`), " ", badge(call.decision, `Vault: ${call.decision}`)),
    details([
      ["Vault path", el("code", {}, call.vault_path)],
      ["In the task scope", call.in_task_scope ? "yes" : "no, not consented"],
      ["Vault said", call.vault_error],
      ["Decided by", call.decision === "refused"
        ? DECISION_POINTS[call.decided_by] ?? "not reported by Vault"
        : null],
      ["MariaDB user", call.db_user ? el("code", {}, call.db_user) : null],
      ["Lease TTL", call.lease_ttl === null ? null : `${call.lease_ttl} s`],
      ["Rows read", call.rows],
      ["MariaDB said", call.db_error],
    ]));
}

function details(rows) {
  const list = el("dl");
  for (const [term, value] of rows) {
    if (value !== null && value !== undefined) list.append(el("dt", {}, term), el("dd", {}, value));
  }
  return list;
}

function badge(kind, label) {
  return el("span", { class: `badge ${kind}` }, label);
}

function codes(values) {
  const span = el("span");
  values.forEach((value, i) => span.append(...(i ? [" "] : []), el("code", {}, value)));
  return span;
}

// --- Start ---------------------------------------------------------------

$("ask").addEventListener("submit", (event) => {
  event.preventDefault();
  const prompt = $("prompt").value.trim();
  if (!prompt) return;
  $("prompt").value = "";
  ask(prompt);
});

for (const button of document.querySelectorAll(".suggestions button")) {
  button.addEventListener("click", () => ask(button.dataset.prompt));
}

api("/api/me")
  .then((me) => showApp(me.persona))
  .catch((error) => showLogin(error instanceof LoginRequired ? null : `The chat failed: ${error.message}`));
