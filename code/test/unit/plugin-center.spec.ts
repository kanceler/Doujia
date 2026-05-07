import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = join(__dirname, '..', '..');

describe('plugin center wiring', () => {
  it('replaces the old template page data sources with the live plugin center APIs', () => {
    const pageSource = readFileSync(
      join(root, 'client/src/pages/TemplatesPage/TemplatesPage.tsx'),
      'utf8',
    );
    const clientSource = readFileSync(
      join(root, 'client/src/api/devflow-client.ts'),
      'utf8',
    );

    expect(pageSource).toContain('getDevflowPluginRegistryState');
    expect(pageSource).toContain('uploadDevflowPluginPack');
    expect(pageSource).toContain('uploadDevflowPipeline');
    expect(pageSource).toContain('getDevflowPluginValidationResult');
    expect(pageSource).not.toContain('getRegistrationStates');
    expect(pageSource).not.toContain('getPipelineTemplates');
    expect(pageSource).not.toContain('saveConfig');
    expect(clientSource).toContain('/api/plugins/registry-state');
  });

  it('splits plugin center into runtime registry, upload, and validation panels', () => {
    const registryPanelSource = readFileSync(
      join(root, 'client/src/pages/TemplatesPage/PluginRegistryPanel.tsx'),
      'utf8',
    );
    const uploadPanelSource = readFileSync(
      join(root, 'client/src/pages/TemplatesPage/PluginUploadPanel.tsx'),
      'utf8',
    );
    const validationPanelSource = readFileSync(
      join(root, 'client/src/pages/TemplatesPage/PluginValidationPanel.tsx'),
      'utf8',
    );

    expect(registryPanelSource).toContain('handlers');
    expect(registryPanelSource).toContain('pipelines');
    expect(uploadPanelSource).toContain('plugin pack');
    expect(uploadPanelSource).toContain('pipeline json');
    expect(validationPanelSource).toContain('activated_pipelines');
    expect(validationPanelSource).toContain('status');
  });
});
