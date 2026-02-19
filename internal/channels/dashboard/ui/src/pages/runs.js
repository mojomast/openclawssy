import { renderJSONViewer } from "../ui/components/json_viewer.js";

const STATUS_FILTERS = [
  { value: "", label: "All statuses" },
  { value: "queued", label: "Queued" },
  { value: "running", label: "Running" },
  { value: "completed", label: "Completed" },
  { value: "failed", label: "Failed" },
  { value: "canceled", label: "Canceled" },
];

const LIMIT_OPTIONS = [10, 25, 50];
const TOOL_ARGS_PREVIEW_CHARS = 140;
const TOOL_ERROR_KEY_CHARS = 260;

const runsViewState = {
  status: "",
  limit: 10,
  offset: 0,
  total: 0,
  runs: [],
  selectedRunID: "",
  selectedRun: null,
  selectedTrace: null,
  selectedTool: null,
  listLoading: false,
  runLoading: false,
  listError: null,
  runError: null,
  hasLoadedList: false,
  listQueryKey: "",
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

function buildListQuery({ status, limit, offset }) {
  const params = new URLSearchParams();
  if (status) {
    params.set("status", status);
  }
  params.set("limit", String(limit));
  params.set("offset", String(offset));
  return `/v1/runs?${params.toString()}`;
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

function selectFirstToolExecution(trace) {
  const items = normalizeToolExecutionResults(trace);
  if (!items.length) {
    return null;
  }
  return toSelectedTool(items[0]);
}

function buildSelectedTrace(run, tracePayload) {
  const debugTrace =
    tracePayload && typeof tracePayload === "object" && tracePayload.trace && typeof tracePayload.trace === "object"
      ? tracePayload.trace
      : null;
  const runTrace = run && typeof run.trace === "object" ? run.trace : null;
  const chosenTrace = debugTrace || runTrace;
  const source = debugTrace ? "debug" : runTrace ? "run" : "none";
  const sourceNote =
    source === "debug"
      ? "Using debug trace from /api/admin/debug/runs/{id}/trace."
      : source === "run"
        ? "Using run trace from /v1/runs/{id}; debug trace unavailable."
        : "No trace found in debug endpoint or run payload.";
  if (!chosenTrace) {
    return {
      run_id: run.id,
      status: run.status,
      message: "Trace unavailable for this run.",
      _dashboard_meta: {
        source,
        source_note: sourceNote,
        run_status: run.status || "",
      },
    };
  }
  return {
    run_id: run.id,
    ...chosenTrace,
    _dashboard_meta: {
      source,
      source_note: sourceNote,
      run_status: run.status || "",
    },
  };
}

function isNotFoundError(err) {
  return err && Number(err.status) === 404;
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

function parseMaybeJSON(value) {
  if (value === null || value === undefined) {
    return null;
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

function formatDuration(ms) {
  const value = Number(ms);
  if (!Number.isFinite(value) || value <= 0) {
    return "-";
  }
  if (value < 1000) {
    return `${value}ms`;
  }
  return `${(value / 1000).toFixed(2)}s`;
}

function modelIdentityTextForRun(run, adminStatus) {
  const runProvider = String(run?.provider || "").trim();
  const runModel = String(run?.model || "").trim();
  if (runProvider || runModel) {
    return `Model identity: ${runProvider || "unknown"} / ${runModel || "unknown"} (captured on this run).`;
  }
  const provider = String(adminStatus?.provider || "").trim();
  const model = String(adminStatus?.model || "").trim();
  if (provider || model) {
    return `Model identity: ${provider || "unknown"} / ${model || "unknown"} (current config).`;
  }
  return "Model identity: unknown.";
}

function formatArgsPreview(argumentsText) {
  const parsed = parseMaybeJSON(argumentsText);
  if (parsed !== null) {
    return compactText(JSON.stringify(parsed), TOOL_ARGS_PREVIEW_CHARS);
  }
  const fallback = compactText(argumentsText, TOOL_ARGS_PREVIEW_CHARS);
  return fallback || "(no args)";
}

function normalizeToolExecutionItem(item, index) {
  if (!item || typeof item !== "object") {
    return null;
  }
  const tool = String(item.tool || "unknown.tool");
  const error = String(item.error || "").trim();
  const callbackError = String(item.callback_error || "").trim();
  const status = error || callbackError ? "failed" : "ok";
  const toolCallID = String(item.tool_call_id || "").trim();
  const normalized = {
    tool,
    tool_call_id: toolCallID,
    duration_ms: Number(item.duration_ms) || 0,
    arguments: typeof item.arguments === "string" ? item.arguments : String(item.arguments || ""),
    output: typeof item.output === "string" ? item.output : String(item.output || ""),
    summary: String(item.summary || "").trim(),
    error,
    callback_error: callbackError,
    status,
    timeline_index: index,
  };
  normalized.selection_key = toolCallID
    ? `${tool}|${toolCallID}`
    : `${tool}|${index}|${normalized.duration_ms}|${normalized.arguments}`;
  return normalized;
}

function normalizeToolExecutionResults(trace) {
  if (!trace || typeof trace !== "object") {
    return [];
  }
  if (!Array.isArray(trace.tool_execution_results)) {
    return [];
  }
  return trace.tool_execution_results
    .map((item, index) => normalizeToolExecutionItem(item, index))
    .filter((item) => item !== null);
}

function toSelectedTool(item) {
  if (!item || typeof item !== "object") {
    return null;
  }
  const parsedArgs = parseMaybeJSON(item.arguments);
  return {
    tool: item.tool,
    tool_call_id: item.tool_call_id,
    duration_ms: item.duration_ms,
    status: item.status,
    arguments: item.arguments,
    arguments_json: parsedArgs,
    args_preview: formatArgsPreview(item.arguments),
    output: item.output,
    summary: item.summary,
    error: item.error,
    callback_error: item.callback_error,
    timeline_index: item.timeline_index,
    selection_key: item.selection_key,
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
  return `${tool}|${Number(toolLike.timeline_index) || 0}`;
}

function groupedTimelineBlocks(toolEntries) {
  const blocks = [];
  let index = 0;
  while (index < toolEntries.length) {
    const current = toolEntries[index];
    const errorKey = compactText(current.error || current.callback_error, TOOL_ERROR_KEY_CHARS);
    if (current.status !== "failed" || !errorKey) {
      blocks.push({ type: "tool", item: current });
      index += 1;
      continue;
    }

    let end = index + 1;
    while (end < toolEntries.length) {
      const candidate = toolEntries[end];
      const candidateError = compactText(candidate.error || candidate.callback_error, TOOL_ERROR_KEY_CHARS);
      if (candidate.status !== "failed" || candidate.tool !== current.tool || candidateError !== errorKey) {
        break;
      }
      end += 1;
    }

    if (end - index > 1) {
      blocks.push({
        type: "failure-group",
        tool: current.tool,
        error: errorKey,
        items: toolEntries.slice(index, end),
      });
    } else {
      blocks.push({ type: "tool", item: current });
    }
    index = end;
  }
  return blocks;
}

export const runsPage = {
  key: "runs",
  title: "Runs",
  async render({ container, apiClient, store, state }) {
    container.innerHTML = "";
    const heading = document.createElement("h2");
    heading.textContent = "Runs";

    const note = document.createElement("p");
    note.className = "muted";
    note.textContent = "Browse runs, filter by status, and inspect trace/tool data from selected runs.";

    const controls = document.createElement("div");
    controls.className = "runs-controls";

    const statusLabel = document.createElement("label");
    statusLabel.className = "runs-control";
    statusLabel.textContent = "Status";
    const statusSelect = document.createElement("select");
    for (const option of STATUS_FILTERS) {
      const item = document.createElement("option");
      item.value = option.value;
      item.textContent = option.label;
      if (runsViewState.status === option.value) {
        item.selected = true;
      }
      statusSelect.append(item);
    }
    statusLabel.append(statusSelect);

    const limitLabel = document.createElement("label");
    limitLabel.className = "runs-control";
    limitLabel.textContent = "Page size";
    const limitSelect = document.createElement("select");
    for (const option of LIMIT_OPTIONS) {
      const item = document.createElement("option");
      item.value = String(option);
      item.textContent = String(option);
      if (runsViewState.limit === option) {
        item.selected = true;
      }
      limitSelect.append(item);
    }
    limitLabel.append(limitSelect);

    const pagination = document.createElement("div");
    pagination.className = "runs-pagination";
    const prevButton = document.createElement("button");
    prevButton.type = "button";
    prevButton.textContent = "Prev";
    const nextButton = document.createElement("button");
    nextButton.type = "button";
    nextButton.textContent = "Next";
    const pageMeta = document.createElement("span");
    pageMeta.className = "runs-page-meta muted";
    pagination.append(prevButton, nextButton, pageMeta);

    controls.append(statusLabel, limitLabel, pagination);

    const listWrap = document.createElement("div");
    listWrap.className = "runs-list";
    listWrap.setAttribute("aria-live", "polite");

    const selectionWrap = document.createElement("div");
    selectionWrap.className = "runs-selection";

    container.append(heading, note, controls, listWrap, selectionWrap);

    function renderList() {
      listWrap.innerHTML = "";

      if (runsViewState.listLoading) {
        const loading = document.createElement("p");
        loading.className = "muted";
        loading.textContent = "Loading runs...";
        loading.setAttribute("role", "status");
        listWrap.append(loading);
      } else if (runsViewState.listError) {
        const error = document.createElement("p");
        error.className = "muted";
        error.textContent = `Failed to load runs: ${runsViewState.listError.message || "unknown error"}`;
        listWrap.append(error);
      } else if (!runsViewState.runs.length) {
        const empty = document.createElement("p");
        empty.className = "muted";
        empty.textContent = "No runs found for this filter.";
        listWrap.append(empty);
      } else {
        const table = document.createElement("table");
        table.className = "runs-table";

        const thead = document.createElement("thead");
        const headerRow = document.createElement("tr");
        for (const label of ["ID", "Status", "Agent", "Updated", "Action"]) {
          const th = document.createElement("th");
          th.textContent = label;
          headerRow.append(th);
        }
        thead.append(headerRow);

        const tbody = document.createElement("tbody");
        for (const run of runsViewState.runs) {
          const row = document.createElement("tr");
          if (run.id === runsViewState.selectedRunID) {
            row.classList.add("selected");
          }

          const idCell = document.createElement("td");
          const idCode = document.createElement("code");
          idCode.textContent = run.id || "-";
          idCell.append(idCode);

          const statusCell = document.createElement("td");
          const statusBadge = document.createElement("span");
          statusBadge.className = "status-badge";
          const status = (run.status || "-").toLowerCase();
          statusBadge.textContent = run.status || "-";
          if (["running", "completed", "failed", "queued"].includes(status)) {
            statusBadge.classList.add(status);
          }
          statusCell.append(statusBadge);

          const agentCell = document.createElement("td");
          agentCell.textContent = run.agent_id || "-";

          const updatedCell = document.createElement("td");
          updatedCell.textContent = formatDateTime(run.updated_at);

          const actionCell = document.createElement("td");
          const openButton = document.createElement("button");
          openButton.type = "button";
          openButton.textContent = run.id === runsViewState.selectedRunID ? "Reload" : "Open";
          openButton.setAttribute("aria-label", `${openButton.textContent} run ${run.id}`);
          openButton.addEventListener("click", () => {
            openRun(run.id);
          });
          actionCell.append(openButton);

          row.append(idCell, statusCell, agentCell, updatedCell, actionCell);
          tbody.append(row);
        }

        table.append(thead, tbody);
        listWrap.append(table);
      }

      const currentPageStart = runsViewState.total === 0 ? 0 : runsViewState.offset + 1;
      const currentPageEnd = Math.min(runsViewState.offset + runsViewState.limit, runsViewState.total);
      pageMeta.textContent = `${currentPageStart}-${currentPageEnd} of ${runsViewState.total}`;

      prevButton.disabled = runsViewState.listLoading || runsViewState.offset <= 0;
      nextButton.disabled =
        runsViewState.listLoading || runsViewState.offset + runsViewState.limit >= runsViewState.total;
    }

    function renderSelection() {
      selectionWrap.innerHTML = "";

      if (!runsViewState.selectedRunID) {
        const empty = document.createElement("p");
        empty.className = "muted";
        empty.textContent = "Select a run to inspect details and trace.";
        selectionWrap.append(empty);
        return;
      }

      if (runsViewState.runLoading) {
        const loading = document.createElement("p");
        loading.className = "muted";
        loading.textContent = `Loading run ${runsViewState.selectedRunID}...`;
        selectionWrap.append(loading);
        return;
      }

      if (runsViewState.runError) {
        renderJSONViewer(
          selectionWrap,
          { run_id: runsViewState.selectedRunID, error: toErrorPayload("runs.detail", runsViewState.runError) },
          { title: "Run Load Error" }
        );
        return;
      }

      if (!runsViewState.selectedRun) {
        const fallback = document.createElement("p");
        fallback.className = "muted";
        fallback.textContent = "Run details unavailable.";
        selectionWrap.append(fallback);
        return;
      }

      const runSummary = document.createElement("section");
      runSummary.className = "runs-run-summary";
      const runMeta = document.createElement("p");
      runMeta.className = "muted";
      runMeta.textContent = `Run ${runsViewState.selectedRun.id || "-"} · status ${runsViewState.selectedRun.status || "-"} · updated ${formatDateTime(runsViewState.selectedRun.updated_at)}`;
      const identityMeta = document.createElement("p");
      identityMeta.className = "muted";
      identityMeta.textContent = modelIdentityTextForRun(runsViewState.selectedRun, state?.adminStatus || null);
      runSummary.append(runMeta, identityMeta);
      renderJSONViewer(runSummary, runsViewState.selectedRun, { title: "Selected Run" });

      const trace = runsViewState.selectedTrace;
      const timelineSection = document.createElement("section");
      timelineSection.className = "runs-timeline";
      const timelineTitle = document.createElement("h3");
      timelineTitle.textContent = "Timeline";
      timelineSection.append(timelineTitle);

      const sourceNote = document.createElement("p");
      sourceNote.className = "runs-trace-source muted";
      const sourceMeta = trace && typeof trace === "object" && trace._dashboard_meta ? trace._dashboard_meta : null;
      const source = sourceMeta && sourceMeta.source ? sourceMeta.source : "none";
      const sourceMessage = sourceMeta && sourceMeta.source_note ? sourceMeta.source_note : "Trace source unavailable.";
      sourceNote.textContent = `Trace source: ${source}. ${sourceMessage}`;
      timelineSection.append(sourceNote);

      const modelSection = document.createElement("div");
      modelSection.className = "runs-model-steps";
      const modelTitle = document.createElement("h4");
      modelTitle.textContent = "Model Steps";
      modelSection.append(modelTitle);

      const modelInputs = trace && Array.isArray(trace.model_inputs) ? trace.model_inputs : [];
      if (!modelInputs.length) {
        const emptyModel = document.createElement("p");
        emptyModel.className = "muted";
        emptyModel.textContent = "No model steps captured in this trace.";
        modelSection.append(emptyModel);
      } else {
        const modelList = document.createElement("ol");
        modelList.className = "runs-model-list";
        for (const step of modelInputs) {
          const row = document.createElement("li");
          row.className = "runs-model-item";
          const iter = Number(step?.iteration) || 0;
          const promptLength = Number(step?.prompt_length) || 0;
          const historyFlag = step?.history_injected ? "history" : "no-history";
          const message = compactText(step?.message || "", 220) || "(empty message)";
          row.textContent = `#${iter || "?"} · prompt ${promptLength} chars · ${historyFlag} · ${message}`;
          modelList.append(row);
        }
        modelSection.append(modelList);
      }
      timelineSection.append(modelSection);

      const toolsSection = document.createElement("div");
      toolsSection.className = "runs-tool-timeline";
      const toolsTitle = document.createElement("h4");
      toolsTitle.textContent = "Tool Calls";
      toolsSection.append(toolsTitle);

      const toolEntries = normalizeToolExecutionResults(trace);
      if (!toolEntries.length) {
        const emptyTools = document.createElement("p");
        emptyTools.className = "muted";
        emptyTools.textContent = "No tool calls captured for this run.";
        toolsSection.append(emptyTools);
      } else {
        const selectedKey = toolSelectionKey(runsViewState.selectedTool);
        const timelineBlocks = groupedTimelineBlocks(toolEntries);
        const list = document.createElement("div");
        list.className = "runs-tool-list";

        const renderToolButton = (toolItem) => {
          const button = document.createElement("button");
          button.type = "button";
          button.className = "runs-tool-item";
          const itemKey = toolSelectionKey(toolItem);
          if (itemKey && itemKey === selectedKey) {
            button.classList.add("selected");
          }

          const top = document.createElement("div");
          top.className = "runs-tool-item-top";

          const name = document.createElement("span");
          name.className = "runs-tool-name";
          name.textContent = toolItem.tool || "unknown.tool";

          const meta = document.createElement("span");
          meta.className = "runs-tool-meta";
          meta.textContent = `${toolItem.status === "failed" ? "failed" : "ok"} · ${formatDuration(toolItem.duration_ms)}`;

          top.append(name, meta);

          const args = document.createElement("p");
          args.className = "runs-tool-args";
          args.textContent = `args: ${formatArgsPreview(toolItem.arguments)}`;

          button.append(top, args);

          if (toolItem.summary) {
            const summary = document.createElement("p");
            summary.className = "runs-tool-summary muted";
            summary.textContent = toolItem.summary;
            button.append(summary);
          }

          button.addEventListener("click", () => {
            const selectedTool = toSelectedTool(toolItem);
            runsViewState.selectedTool = selectedTool;
            store.setState({ selectedTool, inspectorTab: "tool" });
          });

          return button;
        };

        for (const block of timelineBlocks) {
          if (block.type === "tool") {
            list.append(renderToolButton(block.item));
            continue;
          }

          const details = document.createElement("details");
          details.className = "runs-tool-failure-group";
          const summary = document.createElement("summary");
          summary.textContent = `${block.items.length} repeated failures · ${block.tool} · ${block.error}`;
          details.append(summary);

          const grouped = document.createElement("div");
          grouped.className = "runs-tool-failure-items";
          for (const groupedItem of block.items) {
            grouped.append(renderToolButton(groupedItem));
          }
          details.append(grouped);
          list.append(details);
        }
        toolsSection.append(list);
      }
      timelineSection.append(toolsSection);

      selectionWrap.append(runSummary, timelineSection);
    }

    async function loadRuns(force = false) {
      const query = buildListQuery({
        status: runsViewState.status,
        limit: runsViewState.limit,
        offset: runsViewState.offset,
      });

      if (!force && runsViewState.hasLoadedList && runsViewState.listQueryKey === query) {
        return;
      }

      runsViewState.listLoading = true;
      runsViewState.listError = null;
      runsViewState.listQueryKey = query;
      renderList();

      try {
        const payload = await apiClient.get(query);
        runsViewState.runs = Array.isArray(payload?.runs) ? payload.runs : [];
        runsViewState.total = Number(payload?.total) || runsViewState.runs.length;
        runsViewState.offset = Number(payload?.offset) || 0;
        runsViewState.limit = Number(payload?.limit) || runsViewState.limit;
        runsViewState.listError = null;
        runsViewState.hasLoadedList = true;
      } catch (err) {
        runsViewState.listError = err;
        runsViewState.runs = [];
        runsViewState.total = 0;
        updateLastError(store, toErrorPayload("runs.list", err));
      } finally {
        runsViewState.listLoading = false;
        renderList();
      }
    }

    async function openRun(runID) {
      runsViewState.selectedRunID = runID;
      runsViewState.runLoading = true;
      runsViewState.runError = null;
      runsViewState.selectedRun = null;
      runsViewState.selectedTrace = null;
      runsViewState.selectedTool = null;
      store.setState({ selectedTrace: null, selectedTool: null });
      renderList();
      renderSelection();

      let run;
      try {
        run = await apiClient.get(`/v1/runs/${encodeURIComponent(runID)}`);
      } catch (err) {
        runsViewState.runLoading = false;
        runsViewState.runError = err;
        runsViewState.selectedRun = null;
        runsViewState.selectedTrace = null;
        runsViewState.selectedTool = null;
        store.setState({ selectedTrace: null, selectedTool: null });
        updateLastError(store, toErrorPayload("runs.detail", err, { run_id: runID }));
        renderSelection();
        return;
      }

      let tracePayload = null;
      let traceError = null;
      try {
        tracePayload = await apiClient.get(`/api/admin/debug/runs/${encodeURIComponent(runID)}/trace`);
      } catch (err) {
        if (!isNotFoundError(err)) {
          traceError = err;
        }
      }

      const selectedTrace = buildSelectedTrace(run, tracePayload);
      const selectedTool = selectFirstToolExecution(selectedTrace);

      runsViewState.runLoading = false;
      runsViewState.runError = null;
      runsViewState.selectedRun = run;
      runsViewState.selectedTrace = traceError
        ? {
            ...selectedTrace,
            trace_fetch_error: toErrorPayload("runs.trace", traceError, { run_id: runID }),
          }
        : selectedTrace;
      runsViewState.selectedTool = selectedTool;

      const fetchError = traceError ? toErrorPayload("runs.trace", traceError, { run_id: runID }) : null;
      const runError = run.error
        ? {
            scope: "runs.run",
            run_id: runID,
            status: run.status || "",
            message: String(run.error),
          }
        : null;

      store.setState({
        selectedTrace: runsViewState.selectedTrace,
        selectedTool,
        lastError: fetchError || runError || null,
      });

      renderList();
      renderSelection();
    }

    statusSelect.addEventListener("change", () => {
      runsViewState.status = statusSelect.value;
      runsViewState.offset = 0;
      runsViewState.hasLoadedList = false;
      loadRuns(true);
    });

    limitSelect.addEventListener("change", () => {
      const parsed = Number(limitSelect.value);
      runsViewState.limit = Number.isFinite(parsed) && parsed > 0 ? parsed : 10;
      runsViewState.offset = 0;
      runsViewState.hasLoadedList = false;
      loadRuns(true);
    });

    prevButton.addEventListener("click", () => {
      if (runsViewState.offset <= 0) {
        return;
      }
      runsViewState.offset = Math.max(0, runsViewState.offset - runsViewState.limit);
      runsViewState.hasLoadedList = false;
      loadRuns(true);
    });

    nextButton.addEventListener("click", () => {
      if (runsViewState.offset + runsViewState.limit >= runsViewState.total) {
        return;
      }
      runsViewState.offset += runsViewState.limit;
      runsViewState.hasLoadedList = false;
      loadRuns(true);
    });

    renderList();
    renderSelection();
    await loadRuns(false);
  },
};
