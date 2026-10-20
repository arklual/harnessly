// Links the person pinned to the sidebar (the runtime's /api/sidebar-items).
export interface ISidebarItem {
  id: string;
  name: string;
  url: string;
  icon?: string;
}

export interface ISidebarItemsData {
  items: ISidebarItem[];
}
