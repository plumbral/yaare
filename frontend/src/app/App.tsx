import Workbench from './Workbench'
import { ToastViewport } from '@astryxdesign/core/Toast'
export default function App() {
  return <ToastViewport position="bottomEnd" maxVisible={3}><Workbench /></ToastViewport>
}
