const SESSION_LIMIT_OPTIONS = [10, 25, 50];
const MESSAGE_LIMIT_OPTIONS = [50, 200, 500, 1000];
const SESSION_SORT_OPTIONS = [
  { value: "recent", label: "Recently Updated" },
  { value: "oldest", label: "Oldest Updated" },
  { value: "title", label: "Title (A-Z)" },
  { value: "id", label: "Session ID (A-Z)" },
];

const sessionsViewState = {
  limit: 25,
  offset: 0,
  total: 0,
  sessions: [],
  hasLoadedList: false,
  listLoading: false,
  listError: null,
  listQueryKey: "",

  searchQuery: "",
  sortMode: "recent",

  selectedSessionID: "",
  selectedSession: null,
  messageLimit: 200,
  messages: [],
  messagesLoading: false,
  messagesError: null,
  hasLoadedMessages: false,
  messagesQueryKey: "",
};

function formatDateTime(value) {
  if (!value) {
    return "-";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "-";
  }
  return parsed.toLocaleString();
}

export async function fetchRecentSessions(apiClient, limit = 5) {
	const payload = await apiClient.get(buildSessionsListQuery({ limit, offset: 0 }));
	return Array.isArray(payload?.sessions) ? payload.sessions : [];
}

export function renderCompactSessionsList(container, sessions, options = {}) {
	container.innerHTML = "";
	const items = Array.isArray(sessions) ? sessions : [];
	if (!items.length) {
		const empty = document.createElement("p");
		empty.className = "muted";
		empty.textContent = "No chat sessions yet.";
		container.append(empty);
		return;
	}
	const list = document.createElement("div");
	list.className = "widget-list";
	items.slice(0, options.limit || 5).forEach((session) => {
		const row = document.createElement("button");
		row.type = "button";
		row.className = "widget-list-item";
		row.innerHTML = `<strong>${session.title || session.session_id || "session"}</strong><span>${session.agent_id || "default"} · ${formatDateTime(session.updated_at)}</span>`;
		if (typeof options.onOpen === "function") {
			row.addEventListener("click", () => options.onOpen(session));
		}
		list.append(row);
	});
	container.append(list);
}

function compactText(value, maxChars) {
  const text = String(value || "").replace(/\s+/g, " ").trim();
  if (!text) {
    return "";
  }
  if (text.length <= maxChars) {
    return text;
  }
  return `${text.slice(0, Math.max(0, maxChars - 3))}...`;
}

function normalizeSearch(value) {
  return String(value || "")
    .toLowerCase()
    .replace(/\s+/g, " ")
    .trim();
}

function parseMaybeJSON(value) {
  if (value === null || value === undefined) {
    return null;
  }
  if (typeof value === "object") {
    return value;
  }
  const text = String(value).trim();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch (_err) {
    return null;
  }
}

function asDisplayText(value) {
  if (value === null || value === undefined) {
    return "";
  }
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) {
      return "";
    }
    const parsed = parseMaybeJSON(trimmed);
    if (parsed && typeof parsed === "object") {
      try {
        return JSON.stringify(parsed, null, 2);
      } catch (_err) {
        return trimmed;
      }
    }
    return trimmed;
  }
  if (typeof value === "object") {
    try {
      return JSON.stringify(value, null, 2);
    } catch (_err) {
      return String(value);
    }
  }
  return String(value);
}

function firstNonEmpty(...values) {
  for (const value of values) {
    const text = String(value || "").trim();
    if (text) {
      return text;
    }
  }
  return "";
}

function toErrorPayload(scope, err, extra = {}) {
  if (!err) {
    return { scope, message: "Unknown error", ...extra };
  }
  return {
    scope,
    message: err.message || String(err),
    code: err.code || "error.unknown",
    status: Number(err.status) || 0,
    details: err.details || null,
    ...extra,
  };
}

function updateLastError(store, errorLike) {
  const current = JSON.stringify(store.getState().lastError || null);
  const next = JSON.stringify(errorLike || null);
  if (current === next) {
    return;
  }
  store.setState({ lastError: errorLike || null });
}

function buildSessionsListQuery({ limit, offset }) {
  const params = new URLSearchParams();
  params.set("limit", String(limit));
  params.set("offset", String(offset));
  return `/api/admin/chat/sessions?${params.toString()}`;
}

function buildSessionMessagesQuery(sessionID, limit) {
  return `/api/admin/chat/sessions/${encodeURIComponent(sessionID)}/messages?limit=${encodeURIComponent(String(limit))}`;
}

function toToolEvent(message, index) {
  const parsed = parseMaybeJSON(message.content);
  const toolName = firstNonEmpty(message.tool_name, parsed?.tool, parsed?.name, "unknown.tool");
  const toolCallID = firstNonEmpty(message.tool_call_id, parsed?.id, parsed?.tool_call_id);
  const runID = firstNonEmpty(message.run_id, parsed?.run_id);
  const summary = firstNonEmpty(parsed?.summary, parsed?.message);
  const argumentsText = asDisplayText(parsed?.arguments ?? parsed?.args ?? parsed?.input ?? parsed?.params);
  const outputText = asDisplayText(parsed?.output ?? parsed?.result ?? parsed?.response);
  const errorText = asDisplayText(parsed?.error ?? parsed?.callback_error ?? parsed?.stderr);

  return {
    tool: toolName,
    tool_call_id: toolCallID,
    run_id: runID,
    summary,
    arguments_text: argumentsText,
    output_text: outputText || (!parsed ? asDisplayText(message.content) : ""),
    error_text: errorText,
    status: errorText ? "failed" : "ok",
    ts: String(message.ts || ""),
    role: "tool",
    index,
  };
}

function toolSelectionKey(toolLike) {
  if (!toolLike || typeof toolLike !== "object") {
    return "";
  }
  if (toolLike.selection_key) {
    return String(toolLike.selection_key);
  }
  const tool = String(toolLike.tool || "unknown.tool");
  const callID = String(toolLike.tool_call_id || "");
  if (callID) {
    return `${tool}|${callID}`;
  }
  return `${tool}|${String(toolLike.ts || "")}|${Number(toolLike.timeline_index) || Number(toolLike.index) || 0}`;
}

function toSelectedTool(event) {
  if (!event || typeof event !== "object") {
    return null;
  }
  const parsedArgs = parseMaybeJSON(event.arguments_text);
  return {
    tool: event.tool,
    tool_call_id: event.tool_call_id,
    status: event.status,
    duration_ms: 0,
    arguments: event.arguments_text,
    arguments_json: parsedArgs,
    output: event.output_text,
    summary: event.summary,
    error: event.error_text,
    callback_error: "",
    run_id: event.run_id,
    timeline_index: event.index,
    selection_key: toolSelectionKey(event),
  };
}

function sessionSearchBlob(session) {
  return normalizeSearch(
    [session.session_id, session.title, session.agent_id, session.user_id, session.room_id, session.channel]
      .filter((item) => String(item || "").trim())
      .join(" ")
  );
}

function sortSessions(items, sortMode) {
  const sorted = items.slice();
  sorted.sort((a, b) => {
    const aUpdated = new Date(a.updated_at || 0).getTime() || 0;
    const bUpdated = new Date(b.updated_at || 0).getTime() || 0;
    const aCreated = new Date(a.created_at || 0).getTime() || 0;
    const bCreated = new Date(b.created_at || 0).getTime() || 0;
    const aID = String(a.session_id || "");
    const bID = String(b.session_id || "");
    const aTitle = String(a.title || "");
    const bTitle = String(b.title || "");

    if (sortMode === "oldest") {
      if (aUpdated !== bUpdated) {
        return aUpdated - bUpdated;
      }
      return aCreated - bCreated;
    }

    if (sortMode === "title") {
      const titleCmp = aTitle.localeCompare(bTitle);
      if (titleCmp !== 0) {
        return titleCmp;
      }
      if (aUpdated !== bUpdated) {
        return bUpdated - aUpdated;
      }
      return aID.localeCompare(bID);
    }

    if (sortMode === "id") {
      return aID.localeCompare(bID);
    }

    if (aUpdated !== bUpdated) {
      return bUpdated - aUpdated;
    }
    return bCreated - aCreated;
  });
  return sorted;
}

function currentModelIdentityText(adminStatus) {
  const provider = String(adminStatus?.provider || "").trim();
  const model = String(adminStatus?.model || "").trim();
  if (provider || model) {
    return `Model identity: ${provider || "unknown"} / ${model || "unknown"} (current config).`;
  }
  if (adminStatus?.loading) {
    return "Model identity: loading current config...";
  }
  return "Model identity: unknown.";
}

function filteredSortedSessions() {
  const query = normalizeSearch(sessionsViewState.searchQuery);
  const filtered = query
    ? sessionsViewState.sessions.filter((session) => sessionSearchBlob(session).includes(query))
    : sessionsViewState.sessions.slice();
  return sortSessions(filtered, sessionsViewState.sortMode);
}

export const sessionsPage = {
  key: "sessions",
  title: "Sessions",
  async render({ container, apiClient, store, state }) {
    container.innerHTML = "";

    const heading = document.createElement("h2");
    heading.textContent = "Sessions";

    const note = document.createElement("p");
    note.className = "muted";
    note.textContent = "Browse chat sessions, open stored messages, and inspect tool events inline.";

    const controls = document.createElement("div");
    controls.className = "sessions-controls";

    const searchLabel = document.createElement("label");
    searchLabel.className = "sessions-control";
    searchLabel.textContent = "Search";
    const searchInput = document.createElement("input");
    searchInput.type = "search";
    searchInput.placeholder = "Session id, title, user, room";
    searchInput.value = sessionsViewState.searchQuery;
    searchLabel.append(searchInput);

    const sortLabel = document.createElement("label");
    sortLabel.className = "sessions-control";
    sortLabel.textContent = "Sort";
    const sortSelect = document.createElement("select");
    for (const option of SESSION_SORT_OPTIONS) {
      const item = document.createElement("option");
      item.value = option.value;
      item.textContent = option.label;
      if (sessionsViewState.sortMode === option.value) {
        item.selected = true;
      }
      sortSelect.append(item);
    }
    sortLabel.append(sortSelect);

    const limitLabel = document.createElement("label");
    limitLabel.className = "sessions-control";
    limitLabel.textContent = "Page size";
    const limitSelect = document.createElement("select");
    for (const option of SESSION_LIMIT_OPTIONS) {
      const item = document.createElement("option");
      item.value = String(option);
      item.textContent = String(option);
      if (sessionsViewState.limit === option) {
        item.selected = true;
      }
      limitSelect.append(item);
    }
    limitLabel.append(limitSelect);

    const pagination = document.createElement("div");
    pagination.className = "sessions-pagination";
    const prevButton = document.createElement("button");
    prevButton.type = "button";
    prevButton.textContent = "Prev";
    const nextButton = document.createElement("button");
    nextButton.type = "button";
    nextButton.textContent = "Next";
    const pageMeta = document.createElement("span");
    pageMeta.className = "sessions-page-meta muted";
    pagination.append(prevButton, nextButton, pageMeta);

    controls.append(searchLabel, sortLabel, limitLabel, pagination);

    const listWrap = document.createElement("div");
    listWrap.className = "sessions-list";

    const selectionWrap = document.createElement("div");
    selectionWrap.className = "sessions-selection";

    container.append(heading, note, controls, listWrap, selectionWrap);

    function renderList() {
      listWrap.innerHTML = "";

      if (sessionsViewState.listLoading) {
        const loading = document.createElement("p");
        loading.className = "muted";
        loading.textContent = "Loading sessions...";
        listWrap.append(loading);
      } else if (sessionsViewState.listError) {
        const error = document.createElement("p");
        error.className = "muted";
        error.textContent = `Failed to load sessions: ${sessionsViewState.listError.message || "unknown error"}`;
        listWrap.append(error);
      } else {
        const visibleSessions = filteredSortedSessions();
        const filterMeta = document.createElement("p");
        filterMeta.className = "sessions-filter-meta muted";
        filterMeta.textContent = `Showing ${visibleSessions.length} of ${sessionsViewState.sessions.length} sessions on this page.`;
        listWrap.append(filterMeta);

        if (!visibleSessions.length) {
          const empty = document.createElement("p");
          empty.className = "muted";
          empty.textContent = "No sessions match the current search/filter.";
          listWrap.append(empty);
        } else {
          const table = document.createElement("table");
          table.className = "runs-table";

          const thead = document.createElement("thead");
          const headerRow = document.createElement("tr");
          for (const label of ["Session", "Title", "User/Room", "Updated", "Action"]) {
            const th = document.createElement("th");
            th.textContent = label;
            headerRow.append(th);
          }
          thead.append(headerRow);

          const tbody = document.createElement("tbody");
          for (const session of visibleSessions) {
            const row = document.createElement("tr");
            if (session.session_id === sessionsViewState.selectedSessionID) {
              row.classList.add("selected");
            }

            const idCell = document.createElement("td");
            const idCode = document.createElement("code");
            idCode.textContent = session.session_id || "-";
            idCell.append(idCode);

            const titleCell = document.createElement("td");
            titleCell.textContent = session.title || "(untitled)";

            const userRoomCell = document.createElement("td");
            userRoomCell.textContent = `${session.user_id || "-"} / ${session.room_id || "-"}`;

            const updatedCell = document.createElement("td");
            updatedCell.textContent = formatDateTime(session.updated_at);

            const actionCell = document.createElement("td");
            const openButton = document.createElement("button");
            openButton.type = "button";
            openButton.textContent = session.session_id === sessionsViewState.selectedSessionID ? "Reload" : "Open";
            openButton.addEventListener("click", () => {
              openSession(session.session_id, true);
            });
            actionCell.append(openButton);

            row.append(idCell, titleCell, userRoomCell, updatedCell, actionCell);
            tbody.append(row);
          }

          table.append(thead, tbody);
          listWrap.append(table);
        }
      }

      const currentPageStart = sessionsViewState.total === 0 ? 0 : sessionsViewState.offset + 1;
      const currentPageEnd = Math.min(sessionsViewState.offset + sessionsViewState.limit, sessionsViewState.total);
      pageMeta.textContent = `${currentPageStart}-${currentPageEnd} of ${sessionsViewState.total}`;
      prevButton.disabled = sessionsViewState.listLoading || sessionsViewState.offset <= 0;
      nextButton.disabled =
        sessionsViewState.listLoading || sessionsViewState.offset + sessionsViewState.limit >= sessionsViewState.total;
    }

    function renderSelection() {
      selectionWrap.innerHTML = "";

      if (!sessionsViewState.selectedSessionID) {
        const empty = document.createElement("p");
        empty.className = "muted";
        empty.textContent = "Select a session to inspect messages and tool events.";
        selectionWrap.append(empty);
        return;
      }

      const selected = sessionsViewState.selectedSession;
      const header = document.createElement("div");
      header.className = "sessions-selection-header";

      const summary = document.createElement("p");
      summary.className = "muted";
      summary.textContent = `Session ${sessionsViewState.selectedSessionID} · updated ${formatDateTime(selected?.updated_at)} · messages ${sessionsViewState.messages.length}`;
      const identitySummary = document.createElement("p");
      identitySummary.className = "muted";
      identitySummary.textContent = currentModelIdentityText(state?.adminStatus || null);

      const messageLimitLabel = document.createElement("label");
      messageLimitLabel.className = "sessions-control";
      messageLimitLabel.textContent = "Messages";
      const messageLimitSelect = document.createElement("select");
      for (const option of MESSAGE_LIMIT_OPTIONS) {
        const item = document.createElement("option");
        item.value = String(option);
        item.textContent = String(option);
        if (sessionsViewState.messageLimit === option) {
          item.selected = true;
        }
        messageLimitSelect.append(item);
      }
      messageLimitLabel.append(messageLimitSelect);
      header.append(summary, identitySummary, messageLimitLabel);
      selectionWrap.append(header);

      messageLimitSelect.addEventListener("change", () => {
        const parsed = Number(messageLimitSelect.value);
        sessionsViewState.messageLimit = Number.isFinite(parsed) && parsed > 0 ? parsed : 200;
        sessionsViewState.hasLoadedMessages = false;
        openSession(sessionsViewState.selectedSessionID, true);
      });

      if (sessionsViewState.messagesLoading) {
        const loading = document.createElement("p");
        loading.className = "muted";
        loading.textContent = `Loading messages for ${sessionsViewState.selectedSessionID}...`;
        selectionWrap.append(loading);
        return;
      }

      if (sessionsViewState.messagesError) {
        const error = document.createElement("p");
        error.className = "muted";
        error.textContent = `Failed to load messages: ${sessionsViewState.messagesError.message || "unknown error"}`;
        selectionWrap.append(error);
        return;
      }

      if (!sessionsViewState.messages.length) {
        const empty = document.createElement("p");
        empty.className = "muted";
        empty.textContent = "No messages in this session yet.";
        selectionWrap.append(empty);
        return;
      }

      const selectedToolKey = toolSelectionKey(store.getState().selectedTool);
      const stream = document.createElement("div");
      stream.className = "sessions-message-stream";

      sessionsViewState.messages.forEach((message, index) => {
        if (!message || typeof message !== "object") {
          return;
        }

        const role = String(message.role || "").toLowerCase();
        if (role === "user" || role === "assistant") {
          const item = document.createElement("article");
          item.className = `sessions-chat-item sessions-chat-${role}`;

          const top = document.createElement("div");
          top.className = "sessions-chat-meta";
          const roleText = role === "assistant" ? "assistant" : "user";
          top.textContent = `${roleText} · ${formatDateTime(message.ts)}`;

          const body = document.createElement("pre");
          body.className = "sessions-chat-content";
          body.textContent = String(message.content || "");

          item.append(top, body);
          stream.append(item);
          return;
        }

        if (role === "tool") {
          const event = toToolEvent(message, index);
          const eventCard = document.createElement("article");
          eventCard.className = "sessions-tool-event";
          if (event.status === "failed") {
            eventCard.classList.add("failed");
          }

          const top = document.createElement("div");
          top.className = "sessions-tool-top";

          const name = document.createElement("h4");
          name.textContent = event.tool || "unknown.tool";
          const status = document.createElement("span");
          status.className = "sessions-tool-status muted";
          status.textContent = event.status === "failed" ? "failed" : "ok";
          top.append(name, status);

          const meta = document.createElement("p");
          meta.className = "sessions-tool-meta muted";
          const metaParts = [];
          if (event.tool_call_id) {
            metaParts.push(`call ${event.tool_call_id}`);
          }
          if (event.run_id) {
            metaParts.push(`run ${event.run_id}`);
          }
          if (event.ts) {
            metaParts.push(formatDateTime(event.ts));
          }
          meta.textContent = metaParts.join(" · ") || "tool event";

          eventCard.append(top, meta);

          if (event.summary) {
            const summary = document.createElement("p");
            summary.className = "sessions-tool-summary";
            summary.textContent = event.summary;
            eventCard.append(summary);
          }

          const appendField = (label, text, opened = false) => {
            if (!String(text || "").trim()) {
              return;
            }
            const details = document.createElement("details");
            details.className = "sessions-tool-field";
            if (opened) {
              details.open = true;
            }
            const detailsSummary = document.createElement("summary");
            detailsSummary.textContent = label;
            const value = document.createElement("pre");
            value.textContent = text;
            details.append(detailsSummary, value);
            eventCard.append(details);
          };

          appendField("Args", event.arguments_text);
          appendField("Output", event.output_text);
          appendField("Error", event.error_text, true);

          const inspectButton = document.createElement("button");
          inspectButton.type = "button";
          inspectButton.className = "sessions-tool-inspect";
          const eventKey = toolSelectionKey(event);
          if (eventKey && eventKey === selectedToolKey) {
            inspectButton.classList.add("selected");
          }
          inspectButton.textContent = "Inspect Tool";
          inspectButton.addEventListener("click", () => {
            const selectedTool = toSelectedTool(event);
            store.setState({ selectedTool, inspectorTab: "tool" });
          });
          eventCard.append(inspectButton);

          stream.append(eventCard);
          return;
        }

        const unknown = document.createElement("article");
        unknown.className = "sessions-unknown-message";
        const title = document.createElement("p");
        title.className = "muted";
        title.textContent = `message role ${role || "unknown"} · ${formatDateTime(message.ts)}`;
        const payload = document.createElement("pre");
        payload.textContent = compactText(asDisplayText(message.content), 4000) || "(empty message)";
        unknown.append(title, payload);
        stream.append(unknown);
      });

      selectionWrap.append(stream);
    }

    async function loadSessions(force = false) {
      const query = buildSessionsListQuery({ limit: sessionsViewState.limit, offset: sessionsViewState.offset });
      if (!force && sessionsViewState.hasLoadedList && sessionsViewState.listQueryKey === query) {
        return;
      }

      sessionsViewState.listLoading = true;
      sessionsViewState.listError = null;
      sessionsViewState.listQueryKey = query;
      renderList();

      try {
        const payload = await apiClient.get(query);
        sessionsViewState.sessions = Array.isArray(payload?.sessions) ? payload.sessions : [];
        sessionsViewState.total = Number(payload?.total) || sessionsViewState.sessions.length;
        sessionsViewState.limit = Number(payload?.limit) || sessionsViewState.limit;
        sessionsViewState.offset = Number(payload?.offset) || 0;
        sessionsViewState.hasLoadedList = true;
        sessionsViewState.listError = null;
        if (sessionsViewState.selectedSessionID) {
          const selected = sessionsViewState.sessions.find((item) => item.session_id === sessionsViewState.selectedSessionID);
          if (selected) {
            sessionsViewState.selectedSession = selected;
          }
        }
      } catch (err) {
        sessionsViewState.sessions = [];
        sessionsViewState.total = 0;
        sessionsViewState.listError = err;
        updateLastError(store, toErrorPayload("sessions.list", err));
      } finally {
        sessionsViewState.listLoading = false;
        renderList();
      }
    }

    async function openSession(sessionID, force = false) {
      if (!sessionID) {
        return;
      }

      const previousSessionID = sessionsViewState.selectedSessionID;
      const selected = sessionsViewState.sessions.find((item) => item.session_id === sessionID) || sessionsViewState.selectedSession;
      sessionsViewState.selectedSessionID = sessionID;
      sessionsViewState.selectedSession = selected;

      if (previousSessionID !== sessionID) {
        sessionsViewState.hasLoadedMessages = false;
      }

      const query = `${sessionID}|${sessionsViewState.messageLimit}`;
      if (!force && sessionsViewState.hasLoadedMessages && sessionsViewState.messagesQueryKey === query) {
        renderList();
        renderSelection();
        return;
      }

      sessionsViewState.messagesLoading = true;
      sessionsViewState.messagesError = null;
      sessionsViewState.messages = [];

      sessionsViewState.messagesQueryKey = query;
      store.setState({ selectedTool: null });
      renderList();
      renderSelection();

      try {
        const payload = await apiClient.get(buildSessionMessagesQuery(sessionID, sessionsViewState.messageLimit));
        sessionsViewState.messages = Array.isArray(payload?.messages) ? payload.messages : [];
        sessionsViewState.messagesError = null;
        sessionsViewState.hasLoadedMessages = true;
      } catch (err) {
        sessionsViewState.messagesError = err;
        sessionsViewState.messages = [];
        updateLastError(store, toErrorPayload("sessions.messages", err, { session_id: sessionID }));
      } finally {
        sessionsViewState.messagesLoading = false;
        renderList();
        renderSelection();
      }
    }

    searchInput.addEventListener("input", () => {
      sessionsViewState.searchQuery = searchInput.value;
      renderList();
    });

    sortSelect.addEventListener("change", () => {
      sessionsViewState.sortMode = sortSelect.value;
      renderList();
    });

    limitSelect.addEventListener("change", () => {
      const parsed = Number(limitSelect.value);
      sessionsViewState.limit = Number.isFinite(parsed) && parsed > 0 ? parsed : 25;
      sessionsViewState.offset = 0;
      sessionsViewState.hasLoadedList = false;
      loadSessions(true);
    });

    prevButton.addEventListener("click", () => {
      if (sessionsViewState.offset <= 0) {
        return;
      }
      sessionsViewState.offset = Math.max(0, sessionsViewState.offset - sessionsViewState.limit);
      sessionsViewState.hasLoadedList = false;
      loadSessions(true);
    });

    nextButton.addEventListener("click", () => {
      if (sessionsViewState.offset + sessionsViewState.limit >= sessionsViewState.total) {
        return;
      }
      sessionsViewState.offset += sessionsViewState.limit;
      sessionsViewState.hasLoadedList = false;
      loadSessions(true);
    });

    renderList();
    renderSelection();
    await loadSessions(false);
  },
};
