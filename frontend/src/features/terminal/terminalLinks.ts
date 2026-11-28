export function activateTerminalLink(event: MouseEvent, uri: string, open: (url: string) => void) {
  if (event.button === 0 && (event.ctrlKey || event.metaKey)) open(uri)
}
