import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'ELS · 今日学习',
  description: '英语词汇学习体验版',
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="zh-CN"><body>{children}</body></html>;
}
