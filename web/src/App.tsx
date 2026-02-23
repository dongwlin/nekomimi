import { RouterProvider } from 'react-router'
import { appRouter } from './app/router.tsx'

function App() {
  return <RouterProvider router={appRouter} />
}

export default App
