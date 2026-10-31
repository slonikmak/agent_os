import { OpenURL } from '../../bindings/personaldesk/internal/adapters/wailsread/navigationfacade';
import { safeBrowserURL } from './safe-links';
export async function openSafeURL(value: string): Promise<boolean> {
 const target = safeBrowserURL(value);
 if (!target) return false;
 try { return await OpenURL(target); } catch { return false; }
}
