type ByteArrayConstructor = typeof Uint8Array & { fromBase64?: (value: string) => Uint8Array }

export function encodeBase64(bytes:Uint8Array):string{let binary='';for(let i=0;i<bytes.length;i+=8192)binary+=String.fromCharCode(...bytes.subarray(i,i+8192));return btoa(binary)}

export function decodeBase64(value: string): Uint8Array {
  const bytes = Uint8Array as ByteArrayConstructor
  if (typeof bytes.fromBase64 === 'function') return bytes.fromBase64(value)
  return decodeBase64Fallback(value)
}

export function decodeBase64Fallback(value: string): Uint8Array {
  const binary = atob(value)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index)
  return bytes
}
