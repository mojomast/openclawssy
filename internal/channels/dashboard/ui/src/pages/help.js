import {
  HELP_CATEGORIES,
  categoryForTopic,
  getHelpTopicParam,
  loadHelpTopics,
  relatedHelpTopics,
  searchHelpTopics,
  setHelpTopicInHash,
} from "../help/content.js";
import { extractHeadings, renderMarkdownToFragment } from "../help/markdown.js";
import { captureFocusSnapshot, restoreFocusSnapshot } from "../ui/focus_restore.js";

const helpState = {
  container: null,
  topics: [],
  loading: false,
  error: "",
  search: "",
  selectedTopicID: "",
  copyNotice: "",
  copyNoticeTimer: null,
};

function highlightMatch(text, query) {
  const source = String(text || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\"/g, "&quot;")
    .replace(/'/g, "&#39;");
  const q = String(query || "").trim();
  if (!q) {
    return source;
  }
  const escaped = q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return source.replace(new RegExp(`(${escaped})`, "ig"), "<mark>$1</mark>");
}

function rerender(options = {}) {
  if (helpState.container?.isConnected) {
    const focusSnapshot = options.preserveFocus ? captureFocusSnapshot(helpState.container) : null;
    renderHelpPage();
    if (focusSnapshot) {
      restoreFocusSnapshot(helpState.container, focusSnapshot);
    }
  }
}

function showCopyNotice(message) {
  helpState.copyNotice = message;
  if (helpState.copyNoticeTimer) {
    window.clearTimeout(helpState.copyNoticeTimer);
  }
  helpState.copyNoticeTimer = window.setTimeout(() => {
    helpState.copyNotice = "";
    rerender();
  }, 1600);
  rerender();
}

function navigateToTopic(topicID) {
  helpState.selectedTopicID = topicID;
  setHelpTopicInHash(topicID);
  rerender();
}

function selectedTopic() {
  return helpState.topics.find((topic) => topic.id === helpState.selectedTopicID) || helpState.topics[0] || null;
}

async function loadTopics() {
  helpState.loading = true;
  helpState.error = "";
  rerender();
  try {
    helpState.topics = await loadHelpTopics();
    helpState.selectedTopicID = getHelpTopicParam() || helpState.selectedTopicID || helpState.topics[0]?.id || "";
  } catch (error) {
    helpState.error = error instanceof Error ? error.message : String(error);
  } finally {
    helpState.loading = false;
    rerender();
  }
}

function renderTopicList(parent, topics) {
  parent.innerHTML = "";
  HELP_CATEGORIES.forEach((category) => {
    const items = topics.filter((topic) => topic.category === category.key);
    if (!items.length) return;
    const section = document.createElement("section");
    section.className = "help-topic-group";
    const heading = document.createElement("h3");
    heading.textContent = `${category.icon} ${category.label}`;
    section.append(heading);
    items.forEach((topic) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `help-topic-link ${topic.id === helpState.selectedTopicID ? "active" : ""}`;
      button.innerHTML = highlightMatch(topic.title, helpState.search);
      button.addEventListener("click", () => navigateToTopic(topic.id));
      section.append(button);
    });
    parent.append(section);
  });
}

function renderHelpPage() {
  const container = helpState.container;
  container.innerHTML = "";
  const heading = document.createElement("h2");
  heading.textContent = "Help Center";
  const subtitle = document.createElement("p");
  subtitle.className = "muted";
  subtitle.textContent = "Searchable, route-aware guidance you can use alongside the rest of the dashboard.";
  container.append(heading, subtitle);

  if (helpState.copyNotice) {
    const toast = document.createElement("p");
    toast.className = "settings-save-success help-copy-toast";
    toast.textContent = helpState.copyNotice;
    container.append(toast);
  }

  if (helpState.loading) {
    const loading = document.createElement("p");
    loading.className = "muted";
    loading.textContent = "Loading Help Center...";
    container.append(loading);
    return;
  }
  if (helpState.error) {
    const error = document.createElement("p");
    error.className = "settings-inline-error";
    error.textContent = helpState.error;
    container.append(error);
    return;
  }

  const search = document.createElement("input");
  search.type = "search";
  search.className = "settings-input help-search-input";
  search.setAttribute("data-focus-id", "help:center-search");
  search.placeholder = "Search help topics";
  search.value = helpState.search;
  search.addEventListener("input", () => {
    helpState.search = search.value;
    rerender({ preserveFocus: true });
  });
  container.append(search);

  const results = searchHelpTopics(helpState.topics, helpState.search);
  const topic = results.find((item) => item.id === helpState.selectedTopicID) || results[0] || helpState.topics[0] || null;
  if (topic && topic.id !== helpState.selectedTopicID) {
    helpState.selectedTopicID = topic.id;
  }

  const shell = document.createElement("section");
  shell.className = "help-center-shell";
  const sidebar = document.createElement("aside");
  sidebar.className = "help-center-sidebar";
  renderTopicList(sidebar, results);

  const main = document.createElement("article");
  main.className = "help-center-main";
  if (topic) {
    const category = categoryForTopic(topic);
    const headings = extractHeadings(topic.body);
    const breadcrumbs = document.createElement("p");
    breadcrumbs.className = "help-breadcrumbs";
    breadcrumbs.textContent = `${category.label} / ${topic.title}`;
    const title = document.createElement("h3");
    title.textContent = topic.title;
    const actions = document.createElement("div");
    actions.className = "help-topic-actions";
    const copy = document.createElement("button");
    copy.type = "button";
    copy.className = "layout-toggle";
    copy.textContent = "Copy link to topic";
    copy.addEventListener("click", async () => {
      const url = `${window.location.origin}${window.location.pathname}#/help?topic=${encodeURIComponent(topic.id)}`;
      await navigator.clipboard.writeText(url);
      showCopyNotice("Topic link copied.");
    });
    actions.append(copy);
    if (headings.length > 1) {
      const toc = document.createElement("nav");
      toc.className = "help-topic-toc";
      const tocTitle = document.createElement("h4");
      tocTitle.textContent = "On this page";
      toc.append(tocTitle);
      headings.forEach((item) => {
        const link = document.createElement("button");
        link.type = "button";
        link.className = `help-toc-link level-${item.level}`;
        link.textContent = item.title;
        link.addEventListener("click", () => {
          const target = container.querySelector(`#${CSS.escape(item.id)}`);
          target?.scrollIntoView({ behavior: "smooth", block: "start" });
        });
        toc.append(link);
      });
      main.append(breadcrumbs, title, actions, toc);
    } else {
      main.append(breadcrumbs, title, actions);
    }
    const body = document.createElement("div");
    body.className = "help-markdown";
    body.append(renderMarkdownToFragment(topic.body));
    const related = relatedHelpTopics(helpState.topics, topic);
    const relatedWrap = document.createElement("section");
    relatedWrap.className = "help-related-topics";
    const relatedTitle = document.createElement("h4");
    relatedTitle.textContent = "Related topics";
    relatedWrap.append(relatedTitle);
    related.forEach((item) => {
      const link = document.createElement("button");
      link.type = "button";
      link.className = "help-topic-chip";
      link.textContent = item.title;
      link.addEventListener("click", () => navigateToTopic(item.id));
      relatedWrap.append(link);
    });
    main.append(body, relatedWrap);
  }
  shell.append(sidebar, main);
  container.append(shell);
}

export const helpPage = {
  key: "help",
  title: "Help",
  async render({ container }) {
    helpState.container = container;
    helpState.selectedTopicID = getHelpTopicParam() || helpState.selectedTopicID;
    if (!helpState.topics.length && !helpState.loading) {
      await loadTopics();
      return;
    }
    rerender();
  },
};
