// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from 'react';

const MOBILE_BREAKPOINT = 768;

const read = (): boolean => typeof window !== 'undefined' && window.innerWidth < MOBILE_BREAKPOINT;

/** True under 768 px of viewport width. */
export function useIsMobile(): boolean {
  const [mobile, setMobile] = useState(read);
  useEffect(() => {
    const query = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT - 1}px)`);
    const onChange = () => setMobile(read());
    query.addEventListener('change', onChange);
    onChange();
    return () => query.removeEventListener('change', onChange);
  }, []);
  return mobile;
}
