import {desktopAvailable} from './application'
import {SSHConfigService} from '../generated/mygo'
export type {ConfigImportPreview,ConfigImportRow,ConfigImportChoice} from '../generated/mygo'
export const configImportGateway={available:()=>desktopAvailable(),preview:SSHConfigService.preview,commit:SSHConfigService.commit,discard:SSHConfigService.discard}
