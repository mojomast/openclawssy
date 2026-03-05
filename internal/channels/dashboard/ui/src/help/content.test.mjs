import test from 'node:test';
import assert from 'node:assert/strict';

import { parseFrontmatter, searchHelpTopics, stripMarkdown } from './content.js';
import { extractHeadings } from './markdown.js';

test('parseFrontmatter extracts metadata and body', () => {
  const parsed = parseFrontmatter('---\nid: sample\ntitle: Sample\nkeywords: one, two\n---\n\n# Heading\nBody');
  assert.equal(parsed.meta.id, 'sample');
  assert.equal(parsed.meta.title, 'Sample');
  assert.match(parsed.body, /Heading/);
});

test('searchHelpTopics matches title, keywords, and body text', () => {
  const topics = [
    { id: 'one', title: 'Discord Bot Setup', category: 'Integrations', keywords: ['discord', 'token'], plainText: 'Message Content intent is required.' },
    { id: 'two', title: 'Runs', category: 'Debugging', keywords: ['runs'], plainText: 'Inspect traces.' },
  ];
  const results = searchHelpTopics(topics, 'intent');
  assert.equal(results.length, 1);
  assert.equal(results[0].id, 'one');
});

test('stripMarkdown removes markdown formatting for indexing', () => {
  const stripped = stripMarkdown('## Title\n- Item\n`code`\n[Link](https://example.com)');
  assert.match(stripped, /Title/);
  assert.match(stripped, /Item/);
  assert.match(stripped, /code/);
});

test('extractHeadings creates stable ids', () => {
  const headings = extractHeadings('# Intro\n## Setup\n## Setup');
  assert.deepEqual(headings.map((item) => item.id), ['intro', 'setup', 'setup-2']);
});
