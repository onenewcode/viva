import type { Locale } from './i18n';

// Applications that ship on Viva. Each entry is a name, two sentences and the
// facts a reader checks before following a link. The page stays a list, not a
// case study, until one application has more to say than this.
export interface ShowcaseFact { label: string; value: string }
export interface ShowcaseLink { label: string; href: string; primary?: boolean }

// The schema.org node the page publishes for the application. It is the same in
// both languages, so the two dictionaries have to agree on it.
export interface ShowcaseSchema {
  url: string;
  applicationCategory: string;
  applicationSubCategory: string;
  operatingSystem: string;
  license: string;
  codeRepository: string;
  downloadUrl?: string;
}

export interface ShowcaseCase {
  slug: string;
  name: string;
  tagline: string;
  summary: string;
  facts: ShowcaseFact[];
  links: ShowcaseLink[];
  heroAlt: string;
  heroCaption: string;
  schema: ShowcaseSchema;
}

export interface ShowcaseText {
  title: string;
  summary: string;
  cases: ShowcaseCase[];
}

const english: ShowcaseText = {
  title: 'Showcase',
  summary: 'Two desktop applications built with Viva: Motty, an SSH workspace, and Tinglan, an unofficial Huawei Music client.',
  cases: [
    {
      slug: 'motty',
      name: 'Motty',
      tagline: 'A native SSH workspace.',
      summary: 'A terminal, SFTP, port forwarding, remote desktops and an AI assistant in one native window, with no Electron runtime.',
      facts: [
        { label: 'Built with', value: 'Go · Viva' },
        { label: 'Licence', value: 'Apache-2.0' },
        { label: 'Platforms', value: 'macOS · Windows · Linux' },
      ],
      links: [
        { label: 'Visit motty.page', href: 'https://motty.page', primary: true },
        { label: 'Source on GitHub', href: 'https://github.com/t8y2/motty' },
        { label: 'Download', href: 'https://github.com/t8y2/motty/releases' },
      ],
      heroAlt: 'Motty\'s window with a file panel, an SSH terminal and the AI assistant side by side.',
      heroCaption: 'Motty on macOS; the interface is shown in Chinese.',
      schema: {
        url: 'https://motty.page/',
        applicationCategory: 'DeveloperApplication',
        applicationSubCategory: 'SSH client',
        operatingSystem: 'macOS, Windows, Linux',
        license: 'https://www.apache.org/licenses/LICENSE-2.0',
        codeRepository: 'https://github.com/t8y2/motty',
        downloadUrl: 'https://github.com/t8y2/motty/releases',
      },
    },
    {
      slug: 'tinglan',
      name: 'Tinglan',
      tagline: 'An unofficial, experimental Huawei Music client.',
      summary: 'The library, playlists and lyrics in a native window, with twelve themes and a player that can stay docked.',
      facts: [
        { label: 'Built with', value: 'Go · Viva' },
        { label: 'Licence', value: 'AGPL-3.0-only' },
        { label: 'Platform', value: 'macOS (Apple silicon)' },
      ],
      links: [
        { label: 'Source on GitHub', href: 'https://github.com/talongao/tinglan', primary: true },
        { label: 'Issues', href: 'https://github.com/talongao/tinglan/issues' },
      ],
      heroAlt: 'Tinglan\'s lyrics page, with album art on the left and Cantonese lyrics with transliteration on the right.',
      heroCaption: 'Tinglan on macOS. Album art and lyrics belong to their respective owners.',
      schema: {
        url: 'https://github.com/talongao/tinglan',
        applicationCategory: 'MultimediaApplication',
        applicationSubCategory: 'Music player',
        operatingSystem: 'macOS',
        license: 'https://www.gnu.org/licenses/agpl-3.0.html',
        codeRepository: 'https://github.com/talongao/tinglan',
      },
    },
  ],
};

const chinese: ShowcaseText = {
  title: '案例',
  summary: '两个用 Viva 做的桌面应用：SSH 工作台 Motty，以及非官方的华为音乐客户端听澜。',
  cases: [
    {
      slug: 'motty',
      name: 'Motty',
      tagline: '原生 SSH 工作台。',
      summary: 'Motty 把终端、SFTP、端口转发、远程桌面和 AI 助手放进同一个原生窗口，没有 Electron 运行时。',
      facts: [
        { label: '基于', value: 'Go · Viva' },
        { label: '许可证', value: 'Apache-2.0' },
        { label: '平台', value: 'macOS · Windows · Linux' },
      ],
      links: [
        { label: '访问 motty.page', href: 'https://motty.page', primary: true },
        { label: 'GitHub 源码', href: 'https://github.com/t8y2/motty' },
        { label: '下载', href: 'https://github.com/t8y2/motty/releases' },
      ],
      heroAlt: 'Motty 的窗口：文件面板、SSH 终端与 AI 助手并排。',
      heroCaption: 'macOS 上的 Motty，界面为中文。',
      schema: {
        url: 'https://motty.page/',
        applicationCategory: 'DeveloperApplication',
        applicationSubCategory: 'SSH client',
        operatingSystem: 'macOS, Windows, Linux',
        license: 'https://www.apache.org/licenses/LICENSE-2.0',
        codeRepository: 'https://github.com/t8y2/motty',
        downloadUrl: 'https://github.com/t8y2/motty/releases',
      },
    },
    {
      slug: 'tinglan',
      name: 'Tinglan',
      tagline: '非官方、实验性的华为音乐客户端。',
      summary: '音乐库、歌单与歌词都在原生窗口里，12 款主题，播放器可以浮在底部。',
      facts: [
        { label: '基于', value: 'Go · Viva' },
        { label: '许可证', value: 'AGPL-3.0-only' },
        { label: '平台', value: 'macOS（Apple 芯片）' },
      ],
      links: [
        { label: 'GitHub 源码', href: 'https://github.com/talongao/tinglan', primary: true },
        { label: 'Issues', href: 'https://github.com/talongao/tinglan/issues' },
      ],
      heroAlt: '听澜的歌词页：左侧是专辑封面，右侧是粤语歌词与音译。',
      heroCaption: 'macOS 上的听澜。封面与歌词权利归各自权利人所有。',
      schema: {
        url: 'https://github.com/talongao/tinglan',
        applicationCategory: 'MultimediaApplication',
        applicationSubCategory: 'Music player',
        operatingSystem: 'macOS',
        license: 'https://www.gnu.org/licenses/agpl-3.0.html',
        codeRepository: 'https://github.com/talongao/tinglan',
      },
    },
  ],
};

export const showcase = { zh: chinese, en: english } as const;
export function showcaseText(locale: Locale): ShowcaseText {
  return showcase[locale];
}
