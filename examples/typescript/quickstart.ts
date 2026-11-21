/**
 * Kibotos TypeScript SDK quickstart example.
 *
 * Usage:
 *   export KIBOTOS_API_KEY="kb_..."
 *   npx tsx examples/typescript/quickstart.ts
 */
import { Kibotos, UnexpectedStatusError, WaitTimeoutError } from '../../sdks/typescript/src/index.js';

async function main(): Promise<void> {
  const client = new Kibotos();

  // 1. Inspect available zones and templates
  const { data: zones } = await client.zones.list();
  const defaultZone = zones.find((z) => z.default) ?? zones[0];
  console.log('Zones:', zones.map((z) => `${z.id} (${z.status})`).join(', '));

  const { data: templates } = await client.templates.list({ owner: 'platform', limit: 10 });
  const template = templates[0];
  if (!template) {
    throw new Error('No platform templates available');
  }
  console.log(`Using template: ${template.id} (default revision: ${template.default_revision})`);

  // 2. Create a workspace and wait until it is running
  const created = await client.workspaces.create({
    name: 'ts-sdk-quickstart',
    zone: defaultZone?.id,
    template: { id: template.id },
    resources: { vcpus: 2, memory_mib: 4096 },
    timeout: { seconds: 1800, action: 'stop' },
    labels: { example: 'quickstart', language: 'typescript' },
  });
  console.log(`Created workspace ${created.id} (status: ${created.status})`);

  try {
    const running = await client.workspaces.wait(created.id, {
      status: ['running', 'failed'],
      timeoutSeconds: 120,
    });
    console.log(`Workspace ${running.id} reached status: ${running.status}`);

    if (running.status === 'running') {
      // 3. Create a temporary HTTPS exposure for port 8080
      const exposure = await client.exposures.create(running.id, {
        port: 8080,
        ttl_seconds: 600,
      });
      console.log(`Exposed port ${exposure.port} at ${exposure.url}`);

      // 4. List workspaces filtered by label via auto-pagination
      for await (const ws of client.workspaces.iterate({ labels: { example: 'quickstart' } })) {
        console.log(`Matched workspace: ${ws.id} (${ws.status})`);
      }

      // 5. Revoke exposure before tearing down
      await client.exposures.revoke(running.id, exposure.id);
      console.log(`Revoked exposure ${exposure.id}`);
    }
  } catch (err) {
    if (err instanceof WaitTimeoutError) {
      console.error('Timed out waiting for workspace:', err.details);
    } else if (err instanceof UnexpectedStatusError) {
      console.error('Workspace entered unexpected status:', err.details);
    } else {
      throw err;
    }
  } finally {
    // 6. Clean up workspace
    await client.workspaces.delete(created.id);
    console.log(`Deleted workspace ${created.id}`);
  }
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});
