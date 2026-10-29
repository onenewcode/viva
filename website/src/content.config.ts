import { defineCollection } from 'astro:content';
import { glob } from 'astro/loaders';
import { z } from 'astro/zod';

// Guides are written for the website, once per language. The website holds no
// second copy of them that could drift from the guides' source files.
const guides = defineCollection({
  loader: glob({ pattern: '*/*.md', base: './content/guides' }),
  schema: z.object({
    title: z.string(),
    summary: z.string(),
    order: z.number().int().positive(),
    group: z.enum(['start', 'concepts', 'guides', 'distribute', 'more']),
    // The name of an agent skill whose full text this guide shows after its own.
    skill: z.string().optional(),
  }),
});

// The agent skills live in the repository's docs/skills, where a checkout uses them
// directly. The website reads those files in place, so the text it shows and the
// file it serves cannot differ from them.
const skills = defineCollection({
  loader: glob({ pattern: '*/SKILL.md', base: '../docs/skills', generateId: ({ entry }) => entry.split('/')[0] }),
  schema: z.object({ name: z.string(), description: z.string() }),
});

export const collections = { guides, skills };
