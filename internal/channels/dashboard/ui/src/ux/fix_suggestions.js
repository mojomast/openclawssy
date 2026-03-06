const RULES = [
  {
    id: "tool.input_invalid",
    test: (message) =>
      message.includes("tool.input_invalid") ||
      message.includes("missing argument") ||
      message.includes("missing field") ||
      (message.includes("missing") && message.includes("invalid")),
    suggestion: "Tool input is invalid. Open Schema inspector and fill all required fields before retrying.",
    actions: [
      { label: "Open Schema Inspector", kind: "inspector", value: "schema" },
      { label: "Open Tool Inspector", kind: "inspector", value: "tool" },
    ],
  },
  {
    id: "venv.required",
    test: (message) => message.includes("externally-managed-environment"),
    suggestion: "Use a project virtual environment and install packages there.",
    actions: [{ label: "Open Python Env", kind: "inspector", value: "python" }],
  },
  {
    id: "python.module_missing",
    test: (message) => message.includes("modulenotfounderror"),
    suggestion: "Install dependencies inside .venv and run with .venv/bin/python.",
    actions: [{ label: "Open Python Env", kind: "inspector", value: "python" }],
  },
  {
    id: "secrets.env_missing",
    test: (message) =>
      message.includes("env var") ||
      message.includes("environment variable") ||
      message.includes("api key not set") ||
      message.includes("missing required secrets"),
    suggestion: "A required env/secret appears missing. Add or verify key in Secrets page.",
    actions: [{ label: "Open Secrets", kind: "route", value: "/secrets" }],
  },
  {
    id: "provider.timeout",
    test: (message) =>
      message.includes("context deadline exceeded") || message.includes("timeout") || message.includes("timed out"),
    suggestion: "Provider request timed out. Retry first, then consider increasing model timeout_ms in Settings > Model Provider.",
    actions: [{ label: "Open Settings", kind: "route", value: "/settings" }],
  },
];

export function classifyFixSuggestions(errorLike) {
  const source = typeof errorLike === "string" ? errorLike : JSON.stringify(errorLike || {});
  const normalized = source.toLowerCase();
  return RULES.filter((rule) => rule.test(normalized)).map((rule) => ({
    id: rule.id,
    text: rule.suggestion,
    actions: Array.isArray(rule.actions) ? rule.actions : [],
  }));
}

function applyAction(action, store) {
  if (!action || typeof action !== "object") {
    return;
  }
  if (action.kind === "route") {
    window.location.hash = `#${action.value}`;
    return;
  }
  if (action.kind === "inspector" && store?.setState) {
    store.setState({ inspectorTab: action.value });
  }
}

export function renderFixSuggestions(container, errorLike, store) {
  const suggestions = classifyFixSuggestions(errorLike);
  const section = document.createElement("section");
  section.className = "panel-section";

  const title = document.createElement("h3");
  title.textContent = "Fix Suggestions";
  section.append(title);

  if (!suggestions.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No suggestions yet. Triggered errors will be classified here.";
    section.append(empty);
    container.append(section);
    return;
  }

  const list = document.createElement("ul");
  for (const suggestion of suggestions) {
    const item = document.createElement("li");
    const text = document.createElement("p");
    text.textContent = suggestion.text;
    item.append(text);
    if (Array.isArray(suggestion.actions) && suggestion.actions.length) {
      const actions = document.createElement("div");
      actions.className = "chat-composer-actions";
      for (const action of suggestion.actions) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "layout-toggle";
        button.textContent = action.label;
        button.addEventListener("click", () => {
          applyAction(action, store);
        });
        actions.append(button);
      }
      item.append(actions);
    }
    list.append(item);
  }
  section.append(list);
  container.append(section);
}
