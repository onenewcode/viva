import type { APIRoute } from 'astro';
import { readSkill, skillNames } from '../../../lib/skills';

export function getStaticPaths() {
  return skillNames().map(name => ({ params: { name } }));
}

// The raw file, so `curl` can drop it into a project's skills directory.
export const GET: APIRoute = ({ params }) =>
  new Response(readSkill(params.name!), { headers: { 'content-type': 'text/markdown; charset=utf-8' } });
