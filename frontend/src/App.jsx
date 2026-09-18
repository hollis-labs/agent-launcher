import Palette from "./Palette.jsx";
import Manager from "./Manager.jsx";

// One bundle serves both windows; the shell opens each at its own hash. Hash
// routing keeps the asset server a plain file server with no rewrite rules.
export default function App() {
  const hash = window.location.hash;
  if (hash.startsWith("#/manager")) {
    return <Manager route={hash === "#/manager/settings" ? "settings" : "bundle"} />;
  }
  return <Palette />;
}
