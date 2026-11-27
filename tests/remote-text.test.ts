import {expect,test} from 'bun:test'
import {fileLocation,remoteTextError,textLanguage,MAX_REMOTE_TEXT_BYTES} from '../frontend/src/features/workspace/remoteText'

test('remote text language detection uses the filename without evaluating its contents',()=>{
  const cases:Record<string,string>={'/etc/nginx/nginx.conf':'nginx','/srv/APP.YAML':'yaml','/srv/Dockerfile.prod':'dockerfile','/home/user/.bashrc':'shell','/srv/.env.local':'properties','/srv/package.json':'json','/srv/main.go':'go','/srv/note.txt':'text','/srv/script.py':'python'}
  for(const [path,language] of Object.entries(cases))expect(textLanguage(path)).toBe(language)
})
test('remote text limits count UTF-8 bytes and requests stay bound to a session attempt',()=>{
  expect(remoteTextError('中'.repeat(Math.floor(MAX_REMOTE_TEXT_BYTES/3)))).toBe('')
  expect(remoteTextError('中'.repeat(Math.floor(MAX_REMOTE_TEXT_BYTES/3)+1))).not.toBe('')
  expect(fileLocation('session-a',4,'/srv/config.yml')).toEqual({side:'remote',token:'',sessionId:'session-a',attempt:4,path:'/srv/config.yml'})
})
