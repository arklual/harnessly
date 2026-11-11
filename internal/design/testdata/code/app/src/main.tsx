import "@acme/ui/styles.css";
import { createRoot } from "react-dom/client";
import { routes } from "./design/routes";

const route = routes.find((r) => r.path === location.pathname) ?? routes[0];
const Page = route.Component;
createRoot(document.getElementById("root")!).render(<Page />);
