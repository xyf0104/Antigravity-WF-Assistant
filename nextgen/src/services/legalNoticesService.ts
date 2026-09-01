import { invoke } from '@tauri-apps/api/core';

export type LegalNoticeId =
  | 'origin_and_license'
  | 'third_party_notices'
  | 'cc_by_nc_sa_4_0'
  | 'xiass_nextgen_license';

export interface LegalNoticeDocument {
  id: LegalNoticeId;
  title: string;
  content: string;
}

export interface LegalNoticeCollection {
  notices: LegalNoticeDocument[];
}

const LEGAL_NOTICE_IDS: readonly LegalNoticeId[] = [
  'origin_and_license',
  'third_party_notices',
  'cc_by_nc_sa_4_0',
  'xiass_nextgen_license',
];

function isLegalNoticeId(value: unknown): value is LegalNoticeId {
  return typeof value === 'string' && LEGAL_NOTICE_IDS.includes(value as LegalNoticeId);
}

/**
 * Loads the fixed, offline legal-notice catalog shipped with the desktop app.
 * The native command accepts no path or document identifier, so this cannot
 * be used to inspect arbitrary local files.
 */
export async function loadLegalNotices(): Promise<LegalNoticeCollection> {
  const result = await invoke<unknown>('load_legal_notices');
  if (!result || typeof result !== 'object' || !Array.isArray((result as LegalNoticeCollection).notices)) {
    throw new Error('Legal notice catalog is unavailable');
  }

  const notices = (result as LegalNoticeCollection).notices;
  if (notices.length !== LEGAL_NOTICE_IDS.length) {
    throw new Error('Legal notice catalog is incomplete');
  }

  const validatedNotices = notices.map((notice) => {
    if (
      !notice
      || !isLegalNoticeId(notice.id)
      || typeof notice.title !== 'string'
      || !notice.title.trim()
      || typeof notice.content !== 'string'
      || !notice.content.trim()
    ) {
      throw new Error('Legal notice catalog is invalid');
    }
    return { id: notice.id, title: notice.title, content: notice.content };
  });

  const returnedIds = new Set(validatedNotices.map((notice) => notice.id));
  if (
    returnedIds.size !== LEGAL_NOTICE_IDS.length
    || LEGAL_NOTICE_IDS.some((noticeId) => !returnedIds.has(noticeId))
  ) {
    throw new Error('Legal notice catalog is invalid');
  }

  return { notices: validatedNotices };
}
