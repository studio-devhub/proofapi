// App.tsx — root component (Vite)
import RichTextEditor from "./components/RichTextEditor";

export default function App() {
  return (
    <main className="min-h-screen bg-gray-50 flex items-start justify-center pt-16">
      <div className="w-full">
        <div className="max-w-3xl mx-auto px-6 mb-6">
          <h1 className="text-2xl font-bold text-gray-800">Spell Checker</h1>
          <p className="text-sm text-gray-500 mt-1">
            Real-time spell and grammar check — powered by ProofAPI
          </p>
        </div>
        <RichTextEditor />
      </div>
    </main>
  );
}
