import type { DevflowPreviewOperationType } from '@shared/devflow-api';

export const PREVIEW_HANDLER_NAMES = {
  start: 'preview_start',
  browserOpen: 'preview_browser_open',
  inspector: 'preview_inspector',
  console: 'preview_console',
  submitEdits: 'preview_submit_edits',
  repairPrepare: 'preview_repair_prepare',
} as const;

export const PREVIEW_EVENTS = {
  editorMode: 'doujia:editor-mode',
  selection: 'doujia:selection',
  submitEdits: 'doujia:submit-edits',
} as const;

export const PREVIEW_OPERATION_TYPES: DevflowPreviewOperationType[] = [
  'set_text',
  'set_style',
  'set_layout',
  'delete_node',
  'apply_variant',
];

