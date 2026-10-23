import type {ReactNode} from 'react';
import '../frontend/styles/app.css';
import '../frontend/styles/react.css';
export const metadata={title:'Карта IT-работодателей Москвы',manifest:'/manifest.webmanifest',icons:{icon:'/icons/icon-192.png',apple:'/icons/apple-touch-icon.png'},appleWebApp:{capable:true,title:'IT-работодатели',statusBarStyle:'default'}};
export const viewport={width:'device-width',initialScale:1,viewportFit:'cover',themeColor:'#2563eb'};
export default function Layout({children}:{children:ReactNode}){return <html lang="ru"><body>{children}</body></html>;}
