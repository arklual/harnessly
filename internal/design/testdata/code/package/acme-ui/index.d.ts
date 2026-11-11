import type { ReactNode } from "react";

export type Space = "space-2" | "space-4";
export declare function Root(props: { children?: ReactNode }): ReactNode;
export declare function Stack(props: { gap?: Space; children?: ReactNode }): ReactNode;
export declare function Text(props: { size?: "s" | "m" | "l"; tone?: "default" | "muted"; children?: ReactNode }): ReactNode;
export declare function Button(props: { variant?: "primary" | "secondary"; onClick?: () => void; children?: ReactNode }): ReactNode;
export declare function Card(props: { title?: string; children?: ReactNode }): ReactNode;
