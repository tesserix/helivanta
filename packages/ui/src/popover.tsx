"use client";

import { useContext, type ComponentProps } from "react";
import * as PopoverPrimitive from "@radix-ui/react-popover";
import { cn } from "@tesserix/web";
import { BrandThemeContext } from "./brand-theme";

// The app's floating panel (#1255 final review). @tesserix/web's Popover is an
// absolutely positioned div: it clips off a phone screen, has no dialog role
// and drops focus on close. Radix portals it, flips and shifts it to stay in
// the viewport, labels it a dialog the trigger controls, returns focus to the
// trigger, and treats a Select opened inside it as part of it.
export const Popover = PopoverPrimitive.Root;
export const PopoverTrigger = PopoverPrimitive.Trigger;

export function PopoverContent({
  className,
  align = "start",
  sideOffset = 6,
  ...props
}: ComponentProps<typeof PopoverPrimitive.Content>) {
  const theme = useContext(BrandThemeContext);
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content
        data-brand-theme={theme}
        align={align}
        sideOffset={sideOffset}
        collisionPadding={16}
        className={cn(
          "z-50 max-w-[calc(100vw-2rem)] rounded-lg border bg-popover p-4 text-popover-foreground shadow-lg outline-none",
          className,
        )}
        {...props}
      />
    </PopoverPrimitive.Portal>
  );
}
