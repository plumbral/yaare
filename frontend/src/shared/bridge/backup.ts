import {BackupService} from '../generated/mygo'
import {desktopAvailable} from './application'
export type {BackupFile,BackupPreview,BackupResult} from '../generated/mygo'
export const backupGateway={...BackupService,available:()=>desktopAvailable()}
