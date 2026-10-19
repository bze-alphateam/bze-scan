import { Link, type LinkProps } from "@chakra-ui/react";
import type { ReactNode } from "react";
import { LuExternalLink } from "react-icons/lu";

interface ExternalLinkProps extends Omit<LinkProps, "href" | "target" | "rel" | "children"> {
  href: string;
  /** The URL comes from someone else (a validator, a token, a proposal, a markdown body): adds nofollow. */
  userContent?: boolean;
  children: ReactNode;
}

/**
 * Every link that leaves the explorer for something that is not a BZE app
 * (Ping.pub, GitHub, the docs, user-supplied sites): accent text, a 12 px
 * external-link mark, a new tab. Font size comes from the context.
 */
export function ExternalLink({ href, userContent, children, ...props }: ExternalLinkProps) {
  return (
    <Link
      href={href}
      target="_blank"
      rel={userContent ? "noopener noreferrer nofollow" : "noopener noreferrer"}
      color="accent.fg"
      data-testid="external-link"
      {...props}
    >
      {children}
      <LuExternalLink aria-hidden size={12} style={{ display: "inline", flexShrink: 0 }} />
    </Link>
  );
}
