import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// The agent skills live in docs/skills of the repository, where a checkout uses
// them directly. The website serves each file as it is and keeps no copy that
// could drift from it.
const root = () => resolve(process.cwd(), '../docs/skills');

export const skillPath = (name: string): string => `/skills/${name}/SKILL.md`;

export function skillNames(): string[] {
  return readdirSync(root(), { withFileTypes: true }).filter(entry => entry.isDirectory()).map(entry => entry.name).sort();
}

export function readSkill(name: string): string {
  return readFileSync(resolve(root(), name, 'SKILL.md'), 'utf8');
}

// A skill file opens with its own title, and the guide that shows it already has
// one; its sections then sit one level below the guide's "The skill in full".
export function nestSkillHtml(html: string): string {
  return html.replace(/<h1[^>]*>[\s\S]*?<\/h1>\s*/, '').replace(/<(\/?)h2(?=[\s>])/g, '<$1h3');
}
