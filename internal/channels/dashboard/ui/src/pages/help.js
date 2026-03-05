import {
  HELP_CATEGORIES,
  categoryForTopic,
  getHelpTopicParam,
  loadHelpTopics,
  relatedHelpTopics,
  searchHelpTopics,
  setHelpTopicInHash,
} from "../help/content.js";
import { renderMarkdownToFragment } from "../help/markdown.js";

const helpState = {
  container: null,
  topics: [],
  loading: false,
  error: "",
  search: "",
  selectedTopicID: "",
};

function highlightMatch(text, query) {
  const source = String(text || "");
  const q = String(query || "").trim();
  if (!q) {
    return source;
  }
  const escaped = q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return source.replace(new RegExp(`(${escaped})`, "ig"), "<mark>$1</mark>");
}

function rerender() {
  if (helpState.container?.isConnected) {
    renderHelpPage();
  }
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
      button.addEventListener("click", () => {
        helpState.selectedTopicID = topic.id;
        setHelpTopicInHash(topic.id);
        rerender();
      });
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
  search.placeholder = "Search help topics";
  search.value = helpState.search;
  search.addEventListener("input", () => {
    helpState.search = search.value;
    rerender();
    const next = helpState.container?.querySelector?.(".help-search-input");
    next?.focus();
    if (typeof next?.setSelectionRange === "function") {
      next.setSelectionRange(helpState.search.length, helpState.search.length);
    }
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
      copy.textContent = "Link copied";
      setTimeout(() => {
        copy.textContent = "Copy link to topic";
      }, 1200);
    });
    actions.append(copy);
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
      link.className = "help-topic-link";
      link.textContent = item.title;
      link.addEventListener("click", () => {
        helpState.selectedTopicID = item.id;
        setHelpTopicInHash(item.id);
        rerender();
      });
      relatedWrap.append(link);
    });
    main.append(breadcrumbs, title, actions, body, relatedWrap);
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
