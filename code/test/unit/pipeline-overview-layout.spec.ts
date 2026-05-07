import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = join(__dirname, '..', '..');

describe('pipeline overview layout', () => {
  it('lets the current pipeline overview list use the full card height', () => {
    const source = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/PipelineOverviewPanel.tsx'),
      'utf8',
    );

    expect(source).not.toContain('max-h-[330px]');
    expect(source).toContain('flex h-full min-h-0 flex-col');
    expect(source).toContain('mt-1 min-h-0 flex-1 overflow-y-auto');
  });
});
