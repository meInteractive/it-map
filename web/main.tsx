import React from 'react';
import { createRoot } from 'react-dom/client';
import Application from '../frontend/Application';
import '../frontend/styles/app.css';
import '../frontend/styles/react.css';

const target = document.getElementById('root');
if (!target) throw new Error('Missing React root');
createRoot(target).render(<React.StrictMode><Application /></React.StrictMode>);
